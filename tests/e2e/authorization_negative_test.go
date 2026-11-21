package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

// TestAdminBypassRejection verifies that non-superusers cannot bypass admin-only (nil rule)
// collection protections across REST, SQL, and Realtime paths.
func TestAdminBypassRejection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	// 1. Create an admin-only collection with nil rules
	adminCol := core.NewBaseCollection("admin_vault")
	adminCol.Fields.Add(&core.TextField{Name: "secret"})
	adminCol.ListRule = nil
	adminCol.ViewRule = nil
	adminCol.CreateRule = nil
	adminCol.UpdateRule = nil
	adminCol.DeleteRule = nil

	if err := app.Save(adminCol); err != nil {
		t.Fatal(err)
	}

	// 2. Seed an admin record
	record := core.NewRecord(adminCol)
	record.Set("secret", "top-secret-payload")
	if err := app.Save(record); err != nil {
		t.Fatal(err)
	}

	// 3. Obtain user and superuser auth tokens
	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	userToken, err := user.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}

	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	superToken, err := superuser.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}

	// 4. REST API Negative Scenarios: Guest (Unauthenticated) -> 403 Forbidden
	guestScenarios := []tests.ApiScenario{
		{
			Name:                  "Guest List admin_vault -> 403",
			Method:                http.MethodGet,
			URL:                   "/api/collections/admin_vault/records",
			ExpectedStatus:        http.StatusForbidden,
			ExpectedContent:       []string{`"message":"Only superusers can perform this action."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Guest View admin_vault -> 403",
			Method:                http.MethodGet,
			URL:                   "/api/collections/admin_vault/records/" + record.Id,
			ExpectedStatus:        http.StatusForbidden,
			ExpectedContent:       []string{`"message":"Only superusers can perform this action."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Guest Create admin_vault -> 403",
			Method:                http.MethodPost,
			URL:                   "/api/collections/admin_vault/records",
			Body:                  strings.NewReader(`{"secret":"hacked"}`),
			ExpectedStatus:        http.StatusForbidden,
			ExpectedContent:       []string{`"message":"Only superusers can perform this action."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Guest Update admin_vault -> 403",
			Method:                http.MethodPatch,
			URL:                   "/api/collections/admin_vault/records/" + record.Id,
			Body:                  strings.NewReader(`{"secret":"tampered"}`),
			ExpectedStatus:        http.StatusForbidden,
			ExpectedContent:       []string{`"message":"Only superusers can perform this action."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Guest Delete admin_vault -> 403",
			Method:                http.MethodDelete,
			URL:                   "/api/collections/admin_vault/records/" + record.Id,
			ExpectedStatus:        http.StatusForbidden,
			ExpectedContent:       []string{`"message":"Only superusers can perform this action."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
	}
	for _, s := range guestScenarios {
		s.Test(t)
	}

	// 5. REST API Negative Scenarios: Regular Authenticated User -> 403 Forbidden
	userScenarios := []tests.ApiScenario{
		{
			Name:                  "User List admin_vault -> 403",
			Method:                http.MethodGet,
			URL:                   "/api/collections/admin_vault/records",
			Headers:               map[string]string{"Authorization": userToken},
			ExpectedStatus:        http.StatusForbidden,
			ExpectedContent:       []string{`"message":"Only superusers can perform this action."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "User View admin_vault -> 403",
			Method:                http.MethodGet,
			URL:                   "/api/collections/admin_vault/records/" + record.Id,
			Headers:               map[string]string{"Authorization": userToken},
			ExpectedStatus:        http.StatusForbidden,
			ExpectedContent:       []string{`"message":"Only superusers can perform this action."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
	}
	for _, s := range userScenarios {
		s.Test(t)
	}

	// 6. Superuser Access -> 200 OK
	suScenario := tests.ApiScenario{
		Name:                  "Superuser View admin_vault -> 200",
		Method:                http.MethodGet,
		URL:                   "/api/collections/admin_vault/records/" + record.Id,
		Headers:               map[string]string{"Authorization": superToken},
		ExpectedStatus:        http.StatusOK,
		ExpectedContent:       []string{`"secret":"top-secret-payload"`},
		DisableTestAppCleanup: true,
		TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
	}
	suScenario.Test(t)

	// 7. Realtime Authorization Check: CanAccessRecord returns false for regular user
	userReqInfo := &core.RequestInfo{Auth: user}
	if ok, _ := app.CanAccessRecord(record, userReqInfo, adminCol.ViewRule); ok {
		t.Fatal("expected CanAccessRecord to return false for regular user on admin collection")
	}

	// 8. Direct SQL Negative Check: ruxbase_anon and ruxbase_authenticated have NO SELECT grant on admin_vault
	err = tests.RunWithRLSRole(app, core.RLSAnonRole, "", func(txApp core.App) error {
		var total int
		err := txApp.DB().Select("count(*)").From("admin_vault").Row(&total)
		if err == nil {
			t.Fatal("expected permission denied for ruxbase_anon on admin_vault")
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("expected permission denied error, got: %v", err)
		}
		return nil
	})
	if err != nil && !strings.Contains(err.Error(), "permission denied") {
		t.Fatal(err)
	}
}

// TestSystemTableDirectAccessRejection verifies that external restricted database roles
// (ruxbase_anon and ruxbase_authenticated) cannot query internal system tables.
func TestSystemTableDirectAccessRejection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	systemTables := []string{
		core.CollectionNameSuperusers,
		"_collections",
		"_params",
		"_externalAuths",
	}

	for _, table := range systemTables {
		t.Run("Anon_"+table, func(t *testing.T) {
			err := tests.RunWithRLSRole(app, core.RLSAnonRole, "", func(txApp core.App) error {
				var count int
				err := txApp.DB().Select("count(*)").From(table).Row(&count)
				if err == nil {
					t.Fatalf("expected permission denied for ruxbase_anon on %s", table)
				}
				if !strings.Contains(err.Error(), "permission denied") {
					t.Fatalf("expected 'permission denied', got: %v", err)
				}
				return nil
			})
			if err != nil && !strings.Contains(err.Error(), "permission denied") {
				t.Fatal(err)
			}
		})

		t.Run("Auth_"+table, func(t *testing.T) {
			err := tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, "", func(txApp core.App) error {
				var count int
				err := txApp.DB().Select("count(*)").From(table).Row(&count)
				if err == nil {
					t.Fatalf("expected permission denied for ruxbase_authenticated on %s", table)
				}
				if !strings.Contains(err.Error(), "permission denied") {
					t.Fatalf("expected 'permission denied', got: %v", err)
				}
				return nil
			})
			if err != nil && !strings.Contains(err.Error(), "permission denied") {
				t.Fatal(err)
			}
		})
	}
}

// TestCrossTenantIsolationMatrix exercises cross-tenant boundaries across REST,
// Realtime event filtering, and direct SQL with RLS tokens.
func TestCrossTenantIsolationMatrix(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	// 1. Fetch two distinct users
	users, err := app.FindAllRecords("users", dbx.NewExp("1=1"))
	if err != nil || len(users) < 2 {
		t.Fatalf("expected at least 2 users, got %d", len(users))
	}
	userA := users[0]
	userB := users[1]

	tokenB, err := userB.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}

	rlsTokenB, err := userB.NewRLSToken(0)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create tenant-scoped collection
	rule := "tenant = @request.auth.id"
	tenantCol := core.NewBaseCollection("tenant_vault")
	tenantCol.Fields.Add(
		&core.TextField{Name: "tenant"},
		&core.TextField{Name: "title"},
	)
	tenantCol.ListRule = &rule
	tenantCol.ViewRule = &rule
	tenantCol.CreateRule = &rule
	tenantCol.UpdateRule = &rule
	tenantCol.DeleteRule = &rule

	if err := app.Save(tenantCol); err != nil {
		t.Fatal(err)
	}

	// 3. Seed records for Tenant A and Tenant B
	itemA := core.NewRecord(tenantCol)
	itemA.Set("tenant", userA.Id)
	itemA.Set("title", "Tenant A Document")
	if err := app.Save(itemA); err != nil {
		t.Fatal(err)
	}

	itemB := core.NewRecord(tenantCol)
	itemB.Set("tenant", userB.Id)
	itemB.Set("title", "Tenant B Document")
	if err := app.Save(itemB); err != nil {
		t.Fatal(err)
	}

	// 4. REST API: Tenant B queries
	scenarios := []tests.ApiScenario{
		{
			Name:                  "Tenant B List only returns Item B",
			Method:                http.MethodGet,
			URL:                   "/api/collections/tenant_vault/records",
			Headers:               map[string]string{"Authorization": tokenB},
			ExpectedStatus:        http.StatusOK,
			DisableTestAppCleanup: true,
			ExpectedContent: []string{
				`"title":"Tenant B Document"`,
				`"totalItems":1`,
			},
			NotExpectedContent: []string{
				`"title":"Tenant A Document"`,
			},
			TestAppFactory: func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Tenant B View Item A -> 404 Not Found",
			Method:                http.MethodGet,
			URL:                   "/api/collections/tenant_vault/records/" + itemA.Id,
			Headers:               map[string]string{"Authorization": tokenB},
			ExpectedStatus:        http.StatusNotFound,
			DisableTestAppCleanup: true,
			ExpectedContent:       []string{`"message":"The requested resource wasn't found."`},
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Tenant B Update Item A -> 404 Not Found",
			Method:                http.MethodPatch,
			URL:                   "/api/collections/tenant_vault/records/" + itemA.Id,
			Headers:               map[string]string{"Authorization": tokenB},
			Body:                  strings.NewReader(`{"title":"Compromised"}`),
			ExpectedStatus:        http.StatusNotFound,
			DisableTestAppCleanup: true,
			ExpectedContent:       []string{`"message":"The requested resource wasn't found."`},
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Tenant B Delete Item A -> 404 Not Found",
			Method:                http.MethodDelete,
			URL:                   "/api/collections/tenant_vault/records/" + itemA.Id,
			Headers:               map[string]string{"Authorization": tokenB},
			ExpectedStatus:        http.StatusNotFound,
			DisableTestAppCleanup: true,
			ExpectedContent:       []string{`"message":"The requested resource wasn't found."`},
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Tenant B Spoofed Create for Tenant A -> 400 BadRequest",
			Method:                http.MethodPost,
			URL:                   "/api/collections/tenant_vault/records",
			Headers:               map[string]string{"Authorization": tokenB},
			Body:                  strings.NewReader(fmt.Sprintf(`{"tenant":%q,"title":"Malicious"}`, userA.Id)),
			ExpectedStatus:        http.StatusBadRequest,
			DisableTestAppCleanup: true,
			ExpectedContent:       []string{`"message":"Failed to create record."`},
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
	}
	for _, s := range scenarios {
		s.Test(t)
	}

	// 5. Realtime Authorization Check: Tenant B cannot access Tenant A record
	reqInfoB := &core.RequestInfo{Auth: userB}
	if ok, _ := app.CanAccessRecord(itemA, reqInfoB, tenantCol.ViewRule); ok {
		t.Fatal("expected CanAccessRecord to return false for Tenant B on Tenant A item")
	}

	reqInfoA := &core.RequestInfo{Auth: userA}
	if ok, _ := app.CanAccessRecord(itemA, reqInfoA, tenantCol.ViewRule); !ok {
		t.Fatal("expected CanAccessRecord to return true for Tenant A on Tenant A item")
	}

	// 6. Direct SQL with RLS for Tenant B
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, rlsTokenB, func(txApp core.App) error {
		// Read: only Item B should be returned
		var titles []string
		err := txApp.DB().Select("title").From("tenant_vault").OrderBy("title").Column(&titles)
		if err != nil {
			return err
		}
		if len(titles) != 1 || titles[0] != "Tenant B Document" {
			t.Fatalf("expected only Tenant B document, got: %v", titles)
		}

		// Update Item A: affects 0 rows
		res, err := txApp.DB().NewQuery("UPDATE tenant_vault SET title = 'hacked' WHERE id = {:id}").
			Bind(dbx.Params{"id": itemA.Id}).
			Execute()
		if err != nil {
			return err
		}
		affected, _ := res.RowsAffected()
		if affected != 0 {
			t.Fatalf("expected 0 rows affected when updating Item A as Tenant B, got %d", affected)
		}

		// Delete Item A: affects 0 rows
		res, err = txApp.DB().NewQuery("DELETE FROM tenant_vault WHERE id = {:id}").
			Bind(dbx.Params{"id": itemA.Id}).
			Execute()
		if err != nil {
			return err
		}
		affected, _ = res.RowsAffected()
		if affected != 0 {
			t.Fatalf("expected 0 rows affected when deleting Item A as Tenant B, got %d", affected)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestUnsupportedPolicyConditionsFailClosed verifies that untranslatable or malformed
// access rules strictly fail closed across all paths.
func TestUnsupportedPolicyConditionsFailClosed(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	// 1. Untranslatable rule in RLS (uses @request.headers.*)
	headerRule := "@request.headers.x_custom_token = 'secret-token'"
	untransCol := core.NewBaseCollection("untrans_col")
	untransCol.Fields.Add(&core.TextField{Name: "data"})
	untransCol.ListRule = &headerRule
	untransCol.ViewRule = &headerRule

	if err := app.Save(untransCol); err != nil {
		t.Fatal(err)
	}

	// Seed record
	rec := core.NewRecord(untransCol)
	rec.Set("data", "sensitive-header-data")
	if err := app.Save(rec); err != nil {
		t.Fatal(err)
	}

	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	rlsToken, err := user.NewRLSToken(0)
	if err != nil {
		t.Fatal(err)
	}

	// Verify RLS compiler marked policy as denied (FALSE)
	compiled, err := core.CompileRLS(app, untransCol)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range compiled.Policies {
		if p.Rule == core.RLSRuleList {
			if p.Status != core.RLSStatusDenied {
				t.Fatalf("expected list policy status %q, got %q", core.RLSStatusDenied, p.Status)
			}
			if !strings.Contains(p.SQL, "FALSE") {
				t.Fatalf("expected FALSE in denied policy SQL, got %s", p.SQL)
			}
		}
	}

	// Direct SQL query returns 0 rows (denied by default)
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, rlsToken, func(txApp core.App) error {
		var total int
		if err := txApp.DB().Select("count(*)").From("untrans_col").Row(&total); err != nil {
			return err
		}
		if total != 0 {
			t.Fatalf("expected 0 rows for untranslatable rule in direct SQL, got %d", total)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Malformed rule expression: references non-existent column
	invalidRule := "non_existent_column > 10"
	invalidCol := core.NewBaseCollection("invalid_rule_col")
	invalidCol.Fields.Add(&core.TextField{Name: "data"})
	invalidCol.ListRule = &invalidRule
	invalidCol.ViewRule = &invalidRule

	// Collection save validator rejects invalid rules
	if err := app.Save(invalidCol); err == nil {
		t.Fatal("expected collection save to fail with invalid rule validation error")
	}

	// CanAccessRecord returns false and error for invalid rule
	ok, err := app.CanAccessRecord(rec, &core.RequestInfo{}, &invalidRule)
	if ok || err == nil {
		t.Fatalf("expected false and error for invalid rule, got ok=%v, err=%v", ok, err)
	}
}

// TestForgedIdentityRejection tests rejection of forged JWTs, invalid signatures,
// and unauthorized role escalation in PostgreSQL.
func TestForgedIdentityRejection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	// 1. Forged / invalid JWT in HTTP API auth-refresh -> 401 Unauthorized
	forgedScenarios := []tests.ApiScenario{
		{
			Name:                  "Malformed token -> 401",
			Method:                http.MethodPost,
			URL:                   "/api/collections/users/auth-refresh",
			Headers:               map[string]string{"Authorization": "invalid.jwt.structure"},
			ExpectedStatus:        http.StatusUnauthorized,
			ExpectedContent:       []string{`"message":"The request requires valid record authorization token."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
		{
			Name:                  "Random noise token -> 401",
			Method:                http.MethodPost,
			URL:                   "/api/collections/users/auth-refresh",
			Headers:               map[string]string{"Authorization": "not-a-real-token"},
			ExpectedStatus:        http.StatusUnauthorized,
			ExpectedContent:       []string{`"message":"The request requires valid record authorization token."`},
			DisableTestAppCleanup: true,
			TestAppFactory:        func(t testing.TB) *tests.TestApp { return app },
		},
	}
	for _, s := range forgedScenarios {
		s.Test(t)
	}

	// 2. Direct SQL: ruxbase_rls.set_claims with invalid token -> raises error
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, "", func(txApp core.App) error {
		_, err := txApp.DB().NewQuery("SELECT ruxbase_rls.set_claims('invalid_token', true)").Execute()
		if err == nil {
			t.Fatal("expected error when setting invalid RLS token")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Direct SQL: Attempting unauthorized role escalation from restricted role -> permission denied
	err = tests.RunWithRLSRole(app, core.RLSAuthenticatedRole, "", func(txApp core.App) error {
		_, err := txApp.DB().NewQuery("SET ROLE pg_read_all_data").Execute()
		if err == nil {
			t.Fatal("expected permission denied when attempting SET ROLE pg_read_all_data from ruxbase_authenticated")
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("expected permission denied error, got: %v", err)
		}
		return nil
	})
	if err != nil && !strings.Contains(err.Error(), "permission denied") {
		t.Fatal(err)
	}
}
