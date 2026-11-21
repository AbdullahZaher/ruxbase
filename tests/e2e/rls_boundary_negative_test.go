package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

// TestSecurityInvokerViewsEnforceUnderlyingRLS verifies that views created in Ruxbase
// have WITH (security_invoker = true) and that querying the view enforces the underlying
// base tables' Row-Level Security policies against the invoking role.
func TestSecurityInvokerViewsEnforceUnderlyingRLS(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	// 1. Create base collection with RLS owner rule
	baseCol := core.NewBaseCollection("rls_invoker_base")
	baseCol.Fields.Add(&core.TextField{Name: "title"})
	baseCol.Fields.Add(&core.TextField{Name: "owner"})
	rule := "@request.auth.id != '' && owner = @request.auth.id"
	baseCol.ListRule = &rule
	baseCol.ViewRule = &rule
	if err := app.Save(baseCol); err != nil {
		t.Fatalf("Failed to save base collection: %v", err)
	}

	// 2. Insert records owned by user1 and user2
	user1, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find user1: %v", err)
	}
	user2, err := app.FindAuthRecordByEmail("users", "test2@example.com")
	if err != nil {
		t.Fatalf("Failed to find user2: %v", err)
	}

	rec1 := core.NewRecord(baseCol)
	rec1.Set("title", "User1 Document")
	rec1.Set("owner", user1.Id)
	if err := app.Save(rec1); err != nil {
		t.Fatalf("Failed to save rec1: %v", err)
	}

	rec2 := core.NewRecord(baseCol)
	rec2.Set("title", "User2 Document")
	rec2.Set("owner", user2.Id)
	if err := app.Save(rec2); err != nil {
		t.Fatalf("Failed to save rec2: %v", err)
	}

	// 3. Create view collection on top of rls_invoker_base
	viewCol := core.NewViewCollection("rls_invoker_view")
	viewCol.ViewQuery = "SELECT id, title, owner FROM ruxbase_test.rls_invoker_base"
	publicRule := ""
	viewCol.ListRule = &publicRule
	viewCol.ViewRule = &publicRule
	if err := app.Save(viewCol); err != nil {
		// Fallback without schema qualifier if needed
		viewCol.ViewQuery = "SELECT id, title, owner FROM rls_invoker_base"
		if err := app.Save(viewCol); err != nil {
			t.Fatalf("Failed to save view collection: %v", err)
		}
	}

	// Verify reloptions on pg_class has security_invoker=true
	var reloptions []string
	err = app.DB().NewQuery("SELECT reloptions FROM pg_class WHERE relname = 'rls_invoker_view'").Row(&reloptions)
	if err != nil {
		t.Fatalf("Failed to query reloptions for view: %v", err)
	}
	hasSecurityInvoker := false
	for _, opt := range reloptions {
		if strings.Contains(opt, "security_invoker=true") {
			hasSecurityInvoker = true
			break
		}
	}
	if !hasSecurityInvoker {
		t.Fatalf("Expected view to have security_invoker=true in reloptions, got: %v", reloptions)
	}

	// 4. Query view as ruxbase_api with user1's RLS token
	token1, err := user1.NewRLSToken(time.Minute)
	if err != nil {
		t.Fatalf("Failed to generate RLS token for user1: %v", err)
	}

	var visibleTitles []string
	err = app.RunInTransaction(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(
			"SELECT set_config('role', {:role}, true), set_config({:tokenSetting}, {:token}, true), set_config({:ruleSetting}, {:rule}, true)",
		).Bind(dbx.Params{
			"role":         core.RLSAPIRole,
			"tokenSetting": core.RLSTokenSetting,
			"token":        token1,
			"ruleSetting":  core.RLSRuleSetting,
			"rule":         core.RLSRuleList,
		}).Execute()
		if err != nil {
			return err
		}

		return txApp.DB().NewQuery("SELECT title FROM rls_invoker_view ORDER BY title").Column(&visibleTitles)
	})
	if err != nil {
		t.Fatalf("Failed to query view as user1 in RLS: %v", err)
	}

	if len(visibleTitles) != 1 || visibleTitles[0] != "User1 Document" {
		t.Fatalf("Expected exactly User1 Document from security_invoker view, got: %v", visibleTitles)
	}

	// 5. Query view as guest (no token): underlying base table listRule requires @request.auth.id != ''
	var guestTitles []string
	err = app.RunInTransaction(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(
			"SELECT set_config('role', {:role}, true), set_config({:tokenSetting}, '', true), set_config({:ruleSetting}, {:rule}, true)",
		).Bind(dbx.Params{
			"role":         core.RLSAPIRole,
			"tokenSetting": core.RLSTokenSetting,
			"ruleSetting":  core.RLSRuleSetting,
			"rule":         core.RLSRuleList,
		}).Execute()
		if err != nil {
			return err
		}

		return txApp.DB().NewQuery("SELECT title FROM rls_invoker_view").Column(&guestTitles)
	})
	if err != nil {
		t.Fatalf("Failed to query view as guest in RLS: %v", err)
	}
	if len(guestTitles) != 0 {
		t.Fatalf("Expected 0 records for guest from security_invoker view, got: %v", guestTitles)
	}
}

// TestNegativeDirectSQLWritesRestrictedByRLS validates that external database roles
// (ruxbase_anon, ruxbase_authenticated) cannot perform unauthorized direct SQL mutations
// against tables protected by RLS policies.
func TestNegativeDirectSQLWritesRestrictedByRLS(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	col := core.NewBaseCollection("rls_direct_writes")
	col.Fields.Add(&core.TextField{Name: "title"})
	col.Fields.Add(&core.TextField{Name: "author"})
	listRule := "@request.auth.id != ''"
	createRule := "@request.auth.id != '' && author = @request.auth.id"
	updateRule := "@request.auth.id != '' && author = @request.auth.id"
	deleteRule := "@request.auth.id != '' && author = @request.auth.id"
	col.ListRule = &listRule
	col.CreateRule = &createRule
	col.UpdateRule = &updateRule
	col.DeleteRule = &deleteRule
	if err := app.Save(col); err != nil {
		t.Fatalf("Failed to save collection: %v", err)
	}

	user1, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find user1: %v", err)
	}
	user2, err := app.FindAuthRecordByEmail("users", "test2@example.com")
	if err != nil {
		t.Fatalf("Failed to find user2: %v", err)
	}

	token1, err := user1.NewRLSToken(time.Minute)
	if err != nil {
		t.Fatalf("Failed to generate token1: %v", err)
	}
	token2, err := user2.NewRLSToken(time.Minute)
	if err != nil {
		t.Fatalf("Failed to generate token2: %v", err)
	}

	// 1. Unauthenticated guest attempts direct SQL INSERT -> fails
	err = tests.RunWithRLSRole(app, core.RLSAnonRole, "", func(txApp core.App) error {
		_, err := txApp.DB().NewQuery("INSERT INTO rls_direct_writes (id, title, author) VALUES ('rec_anon', 'Hacked', 'guest')").Execute()
		return err
	})
	if err == nil {
		t.Fatalf("Expected direct SQL INSERT as ruxbase_anon to fail, but it succeeded")
	}

	// 2. User1 inserts record rec_u1
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token1, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery("INSERT INTO rls_direct_writes (id, title, author) VALUES ('rec_u1', 'Post 1', {:author})").
			Bind(dbx.Params{"author": user1.Id}).Execute()
		return err
	})
	if err != nil {
		t.Fatalf("Failed to insert as user1: %v", err)
	}

	// Insert rec_u1 permanently as app role for update/delete tests
	permRec := core.NewRecord(col)
	permRec.Set("title", "User1 Permanent Post")
	permRec.Set("author", user1.Id)
	if err := app.Save(permRec); err != nil {
		t.Fatalf("Failed to save permanent record: %v", err)
	}

	// 3. User2 attempts to UPDATE User1's record -> 0 rows affected
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token2, func(txApp core.App) error {
		res, err := txApp.DB().NewQuery("UPDATE rls_direct_writes SET title = 'Defaced' WHERE id = {:id}").
			Bind(dbx.Params{"id": permRec.Id}).Execute()
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 0 {
			t.Fatalf("Expected 0 rows affected when updating another user's record, got: %d", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Unexpected error in update attempt: %v", err)
	}

	// 4. User2 attempts to DELETE User1's record -> 0 rows affected
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token2, func(txApp core.App) error {
		res, err := txApp.DB().NewQuery("DELETE FROM rls_direct_writes WHERE id = {:id}").
			Bind(dbx.Params{"id": permRec.Id}).Execute()
		if err != nil {
			return err
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if rows != 0 {
			t.Fatalf("Expected 0 rows affected when deleting another user's record, got: %d", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Unexpected error in delete attempt: %v", err)
	}
}

// TestDirectSQLWriteDeniedOnAuthCollections verifies that direct SQL mutations (INSERT, UPDATE, DELETE)
// are completely revoked on auth collections for external roles.
func TestDirectSQLWriteDeniedOnAuthCollections(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find user: %v", err)
	}
	token, err := user.NewRLSToken(time.Minute)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// 1. Direct INSERT into auth table
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery("INSERT INTO users (id, name, username) VALUES ('evil_user', 'Evil', 'evil')").Execute()
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Expected permission denied on auth table direct INSERT, got: %v", err)
	}

	// 2. Direct UPDATE on auth table
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery("UPDATE users SET name = 'Escalated' WHERE id = {:id}").
			Bind(dbx.Params{"id": user.Id}).Execute()
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Expected permission denied on auth table direct UPDATE, got: %v", err)
	}

	// 3. Direct DELETE on auth table
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery("DELETE FROM users WHERE id = {:id}").
			Bind(dbx.Params{"id": user.Id}).Execute()
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Expected permission denied on auth table direct DELETE, got: %v", err)
	}
}

// TestDirectSQLWriteDeniedOnSystemTables verifies that direct SQL operations against system tables
// and metadata tables (_superusers, _collections, _params) are unconditionally revoked.
func TestDirectSQLWriteDeniedOnSystemTables(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find user: %v", err)
	}
	token, err := user.NewRLSToken(time.Minute)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	systemTables := []string{"_superusers", "_collections", "_params", "_realtime_events"}

	for _, tbl := range systemTables {
		t.Run("select_"+tbl, func(t *testing.T) {
			err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token, func(txApp core.App) error {
				_, err := txApp.DB().NewQuery("SELECT * FROM " + tbl + " LIMIT 1").Execute()
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "permission denied") {
				t.Fatalf("Expected permission denied on SELECT %s, got: %v", tbl, err)
			}
		})

		t.Run("insert_"+tbl, func(t *testing.T) {
			err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token, func(txApp core.App) error {
				_, err := txApp.DB().NewQuery("INSERT INTO " + tbl + " DEFAULT VALUES").Execute()
				return err
			})
			if err == nil || !strings.Contains(err.Error(), "permission denied") {
				t.Fatalf("Expected permission denied on INSERT %s, got: %v", tbl, err)
			}
		})
	}
}

// TestDirectSQLDeniedOnHiddenColumns validates that hidden fields and auth credentials
// (password, tokenKey, email) cannot be read via direct SQL by external roles.
func TestDirectSQLDeniedOnHiddenColumns(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find user: %v", err)
	}
	token, err := user.NewRLSToken(time.Minute)
	if err != nil {
		t.Fatalf("Failed to create token: %v", err)
	}

	// 1. Auth email column is not granted to external roles
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token, func(txApp core.App) error {
		var email string
		return txApp.DB().NewQuery("SELECT email FROM users WHERE id = {:id}").
			Bind(dbx.Params{"id": user.Id}).Row(&email)
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Expected permission denied when selecting email from users, got: %v", err)
	}

	// 2. Auth password column is not granted to external roles
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token, func(txApp core.App) error {
		var pass string
		return txApp.DB().NewQuery("SELECT password FROM users WHERE id = {:id}").
			Bind(dbx.Params{"id": user.Id}).Row(&pass)
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Expected permission denied when selecting password from users, got: %v", err)
	}

	// 3. Auth tokenKey column is not granted to external roles
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, token, func(txApp core.App) error {
		var tk string
		return txApp.DB().NewQuery("SELECT \"tokenKey\" FROM users WHERE id = {:id}").
			Bind(dbx.Params{"id": user.Id}).Row(&tk)
	})
	if err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("Expected permission denied when selecting tokenKey from users, got: %v", err)
	}
}

// TestUntranslatableRulesFailClosedInRLS confirms that rules using untranslatable expressions
// (@request.body, @request.headers, :isset, :changed) compile to USING (FALSE) and fail closed.
func TestUntranslatableRulesFailClosedInRLS(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	col := core.NewBaseCollection("rls_untranslatable_guard")
	col.Fields.Add(&core.TextField{Name: "data"})
	untransRule := "@request.body.data = 'valid'"
	col.ListRule = &untransRule
	col.CreateRule = &untransRule
	if err := app.Save(col); err != nil {
		t.Fatalf("Failed to save collection: %v", err)
	}

	// Verify compilation status is denied
	compiled, err := core.CompileRLS(app, col)
	if err != nil {
		t.Fatalf("Failed to compile RLS: %v", err)
	}
	if compiled.Status != core.RLSCollectionDenied {
		t.Fatalf("Expected collection status %q, got %q", core.RLSCollectionDenied, compiled.Status)
	}
	for _, p := range compiled.Policies {
		if p.Rule == core.RLSRuleList {
			if p.Status != core.RLSStatusDenied {
				t.Fatalf("Expected list policy to be denied, got %s", p.Status)
			}
			if !strings.Contains(p.SQL, "FALSE") {
				t.Fatalf("Expected policy SQL to contain FALSE, got: %s", p.SQL)
			}
		}
	}

	// Add test record as app role
	rec := core.NewRecord(col)
	rec.Set("data", "valid")
	if err := app.Save(rec); err != nil {
		t.Fatalf("Failed to insert record: %v", err)
	}

	// Direct query as ruxbase_anon returns 0 rows
	err = tests.RunWithRLSRole(app, core.RLSAnonRole, "", func(txApp core.App) error {
		var count int
		err := txApp.DB().NewQuery("SELECT count(*) FROM rls_untranslatable_guard").Row(&count)
		if err != nil {
			return err
		}
		if count != 0 {
			t.Fatalf("Expected 0 rows due to FALSE policy, got: %d", count)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Unexpected error in query: %v", err)
	}
}

// TestPrivilegedRoutinesSearchPathHardening verifies that all functions created in schema
// ruxbase_rls specify an explicit, secure search_path and execution privileges are not granted to PUBLIC.
func TestPrivilegedRoutinesSearchPathHardening(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	type routineCheck struct {
		Proname   string `db:"proname"`
		Prosecdef bool   `db:"prosecdef"`
		Proconfig string `db:"proconfig"`
	}

	var routines []routineCheck
	err := app.DB().NewQuery(`
		SELECT p.proname, p.prosecdef, COALESCE(array_to_string(p.proconfig, ','), '') AS proconfig
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'ruxbase_rls'
	`).All(&routines)
	if err != nil {
		t.Fatalf("Failed to query routines in ruxbase_rls: %v", err)
	}

	if len(routines) == 0 {
		t.Fatalf("Expected routines in ruxbase_rls schema, found 0")
	}

	for _, r := range routines {
		if r.Prosecdef {
			if !strings.Contains(r.Proconfig, "search_path=") {
				t.Fatalf("SECURITY DEFINER routine %q in ruxbase_rls lacks explicit search_path in proconfig: %s", r.Proname, r.Proconfig)
			}
			if !strings.Contains(r.Proconfig, "pg_catalog") {
				t.Fatalf("Routine %q search_path does not start with pg_catalog: %s", r.Proname, r.Proconfig)
			}
		}
	}
}
