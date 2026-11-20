package tests

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

// ExercisePreparedHTTP uses the original API handlers on a disposable fixture
// over a real loopback TCP listener. It is not a tenant isolation certification. Its
// narrow fixture policies intentionally allow only the test's two tables.
func ExercisePreparedHTTP(t *testing.T, owner *sql.DB, ownerURL, identifier string, config core.BaseAppConfig, manifest pgrolecheck.GrantManifest, identities *pgrolecheck.IdentityContract) {
	t.Helper()
	t.Setenv("RUXBASE_RLS_STRICT", "false")
	admin := core.NewBaseApp(core.BaseAppConfig{
		DatabaseURL:              ownerURL,
		LogsDatabaseURL:          ownerURL,
		DataDir:                  t.TempDir(),
		RealtimeSource:           "hooks",
		OfflineSystemPreparation: true,
		PreprovisionedRLSRoles:   identities != nil,
	})
	defer admin.ClearBootstrap()
	if err := admin.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	users := core.NewAuthCollection("m01_fixture_users")
	users.AuthAlert.Enabled = false // no outbound notifications in the disposable fixture
	if err := admin.Save(users); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := admin.Delete(users); err != nil {
			t.Error("unable to remove disposable fixture auth collection")
		}
	}()
	items := core.NewBaseCollection("m01_fixture_items")
	items.Fields.Add(&core.TextField{Name: "title", Required: true})
	rule := types.Pointer("@request.auth.id != ''")
	items.ListRule, items.ViewRule = rule, rule
	items.CreateRule, items.UpdateRule, items.DeleteRule = rule, rule, rule
	if err := admin.Save(items); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := admin.Delete(items); err != nil {
			t.Error("unable to remove disposable fixture items collection")
		}
	}()
	user := core.NewRecord(users)
	user.SetEmail("fixture@example.test")
	user.SetPassword("fixture-password-123")
	if err := admin.Save(user); err != nil {
		t.Fatal(err)
	}
	// Only these disposable test collections receive access. The runtime
	// role remains unable to create schemas, manage roles or own tables.
	for _, statement := range []string{
		"GRANT SELECT ON public.m01_fixture_users TO " + identifier,
		"GRANT SELECT, INSERT, UPDATE, DELETE ON public.m01_fixture_items TO " + identifier,
		"CREATE POLICY m01_fixture_login ON public.m01_fixture_users FOR SELECT TO " + identifier + " USING (email = 'fixture@example.test')",
		"CREATE POLICY m01_fixture_items ON public.m01_fixture_items TO " + identifier + " USING (title LIKE 'fixture-%') WITH CHECK (title LIKE 'fixture-%')",
	} {
		if _, err := owner.Exec(statement); err != nil {
			t.Fatal("unable to prepare disposable HTTP fixture")
		}
	}

	var verifyWAL func(string)
	combinedWAL := identities != nil && identities.Replication != ""
	if combinedWAL {
		grant := "GRANT INSERT ON public._realtime_events TO " + identifier
		if _, err := owner.Exec(grant); err != nil {
			t.Fatal("unable to prepare event-store fixture grant")
		}
		defer func() {
			revoke := "REVOKE INSERT ON public._realtime_events FROM " + identifier
			if _, err := owner.Exec(revoke); err != nil {
				t.Error("unable to revoke disposable event-store grant")
			}
		}()
	}

	// Build expected grants from the reviewed test policy and declared object
	// names, not from observed runtime ACLs. Copy slices before modifying.
	manifest.Schemas = append([]pgrolecheck.GrantContract(nil), manifest.Schemas...)
	for i := range manifest.Schemas {
		schema := &manifest.Schemas[i]
		if schema.Schema != "public" {
			continue
		}
		schema.Grants = append([]pgrolecheck.Grant(nil), schema.Grants...)
		type targetGrant struct {
			name        string
			permissions []string
		}
		targets := []targetGrant{
			{"m01_fixture_users", []string{"SELECT"}},
			{"m01_fixture_items", []string{"SELECT", "INSERT", "UPDATE", "DELETE"}},
		}
		if combinedWAL {
			targets = append(targets, targetGrant{"_realtime_events", []string{"INSERT"}})
		}
		for _, target := range targets {
			rows, err := owner.Query(`
SELECT pg_catalog.quote_ident(a.attname)
FROM pg_catalog.pg_attribute a
JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relname = $1
  AND a.attnum > 0 AND NOT a.attisdropped
`, target.name)
			if err != nil {
				t.Fatal(err)
			}
			var columns []string
			for rows.Next() {
				var column string
				if err := rows.Scan(&column); err != nil {
					rows.Close()
					t.Fatal(err)
				}
				columns = append(columns, column)
			}
			err = rows.Err()
			rows.Close()
			if err != nil || len(columns) == 0 {
				t.Fatal("unable to enumerate declared fixture columns")
			}
			for _, privilege := range target.permissions {
				schema.Grants = append(schema.Grants, pgrolecheck.Grant{Kind: "relation", Object: target.name, Privilege: privilege})
				if privilege == "DELETE" {
					continue
				}
				for _, column := range columns {
					schema.Grants = append(schema.Grants, pgrolecheck.Grant{Kind: "column", Object: target.name + "." + column, Privilege: privilege})
				}
			}
		}
	}
	raw, err := json.Marshal(struct {
		Main       pgrolecheck.GrantManifest     `json:"main"`
		Auxiliary  pgrolecheck.GrantManifest     `json:"auxiliary"`
		Identities *pgrolecheck.IdentityContract `json:"identities,omitempty"`
	}{manifest, manifest, identities})
	if err != nil {
		t.Fatal(err)
	}
	config.PreparedManifestJSON = string(raw)
	config.DataDir = t.TempDir()
	config.RealtimeSource = "hooks"
	if combinedWAL {
		var stop func()
		verifyWAL, stop = StartPreparedWALFixture(t, owner, config.DatabaseURL, *identities, manifest, items.Id)
		defer stop()
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- apis.ServePreparedLoopback(ctx, listener, config, apis.PreparedLoopbackConfig{
			AuthCollection:    "m01_fixture_users",
			RecordCollections: []string{"m01_fixture_items"},
		})
	}()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("prepared HTTP lifecycle: %v", err)
			}
		case <-time.After(35 * time.Second):
			t.Error("prepared HTTP server failed to stop")
		}
	}()
	client := &http.Client{Timeout: 30 * time.Second}
	defer client.CloseIdleConnections()
	baseURL := "http://" + listener.Addr().String()
	const records = "/api/collections/m01_fixture_items/records"
	const login = "/api/collections/m01_fixture_users/auth-with-password"

	request := func(method, path, body, token string, expected int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, baseURL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != expected {
			t.Fatalf("%s %s: expected %d, got %d", method, path, expected, res.StatusCode)
		}
		return raw
	}

	request(http.MethodGet, "/api/settings", "", "", http.StatusNotFound)
	request(http.MethodGet, "/api/collections/_superusers/records", "", "", http.StatusNotFound)
	request(http.MethodGet, records+"?expand=other", "", "", http.StatusNotFound)
	guest := request(http.MethodGet, records, "", "", http.StatusOK)
	var guestList struct {
		TotalItems int `json:"totalItems"`
	}
	if err := json.Unmarshal(guest, &guestList); err != nil || guestList.TotalItems != 0 {
		t.Fatal("guest list exposed authenticated fixture records")
	}
	request(http.MethodPost, login, `{"identity":"fixture@example.test","password":"incorrect"}`, "", http.StatusBadRequest)
	res := request(http.MethodPost, login, `{"identity":"fixture@example.test","password":"fixture-password-123"}`, "", http.StatusOK)
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res, &session); err != nil || session.Token == "" {
		t.Fatal("fixture authentication did not return a token")
	}
	res = request(http.MethodPost, records, `{"title":"fixture-created"}`, session.Token, http.StatusOK)
	var item struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res, &item); err != nil || item.ID == "" {
		t.Fatal("fixture create did not return a record")
	}
	if verifyWAL != nil {
		verifyWAL(item.ID)
	}
	path := records + "/" + item.ID
	request(http.MethodGet, path, "", session.Token, http.StatusOK)
	request(http.MethodGet, path, "", "", http.StatusNotFound)
	guest = request(http.MethodGet, records, "", "", http.StatusOK)
	if err := json.Unmarshal(guest, &guestList); err != nil || guestList.TotalItems != 0 {
		t.Fatal("guest list exposed a persisted authenticated fixture record")
	}
	request(http.MethodPatch, path, `{"title":"fixture-updated"}`, session.Token, http.StatusOK)
	var storedTitle string
	if err := owner.QueryRow("SELECT title FROM public.m01_fixture_items WHERE id = $1", item.ID).Scan(&storedTitle); err != nil || storedTitle != "fixture-updated" {
		t.Fatal("fixture update was not persisted")
	}
	request(http.MethodDelete, path, "", session.Token, http.StatusNoContent)
	request(http.MethodGet, path, "", session.Token, http.StatusNotFound)
	var remaining int
	if err := owner.QueryRow("SELECT count(*) FROM public.m01_fixture_items").Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal("restricted HTTP fixture did not persist the expected lifecycle")
	}
}

// PreparedFixtureManifest derives object names only for extension and built-in
// public routines in this controlled CI fixture; it never reads the runtime
// ACLs to decide what to approve. The fixture policy explicitly expects
// default PUBLIC EXECUTE on public and auxiliary routines, no rights on ruxbase_rls,
// and only the three metadata table reads plus schema usage. This is not a
// production manifest generator or a serving-role grant recommendation.
func PreparedFixtureManifest(t *testing.T, owner *sql.DB, role string) pgrolecheck.GrantManifest {
	t.Helper()
	main := pgrolecheck.GrantContract{
		Role: role, Schema: "public", Extended: true,
		Grants: []pgrolecheck.Grant{{Kind: "schema", Object: "public", Privilege: "USAGE"}},
	}
	for table, columns := range map[string][]string{
		"_migrations": {"file", "applied"},
		"_params":     {"id", "value", "created", "updated"},
		"_collections": {
			"id", "system", "type", "name", "fields", "indexes",
			"listRule", "viewRule", "createRule", "updateRule", "deleteRule",
			"options", "created", "updated",
		},
	} {
		main.Grants = append(main.Grants, pgrolecheck.Grant{Kind: "relation", Object: table, Privilege: "SELECT"})
		for _, column := range columns {
			var object string
			if err := owner.QueryRow("SELECT pg_catalog.quote_ident($1) || '.' || pg_catalog.quote_ident($2)", table, column).Scan(&object); err != nil {
				t.Fatal("unable to canonicalize reviewed fixture column name")
			}
			main.Grants = append(main.Grants, pgrolecheck.Grant{Kind: "column", Object: object, Privilege: "SELECT"})
		}
	}
	aux := pgrolecheck.GrantContract{
		Role: role, Schema: "_aux", Extended: true,
		Grants: []pgrolecheck.Grant{{Kind: "schema", Object: "_aux", Privilege: "USAGE"}},
	}
	ctx := context.Background()
	tx, err := owner.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = pg_catalog"); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.QueryContext(ctx, `
SELECT n.nspname, pg_catalog.quote_ident(p.proname) || '(' ||
       pg_catalog.pg_get_function_identity_arguments(p.oid) || ')'
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname IN ('public', '_aux')
`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var schema, signature string
		if err := rows.Scan(&schema, &signature); err != nil {
			t.Fatal(err)
		}
		grant := pgrolecheck.Grant{Kind: "routine", Object: signature, Privilege: "EXECUTE"}
		if schema == "_aux" {
			aux.Grants = append(aux.Grants, grant)
		} else {
			main.Grants = append(main.Grants, grant)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return pgrolecheck.GrantManifest{
		Role: role,
		Schemas: []pgrolecheck.GrantContract{
			main,
			aux,
			{Role: role, Schema: "ruxbase_rls", Extended: true, Grants: []pgrolecheck.Grant{}},
		},
	}
}
