package core_test

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

// exercisePreparedHTTP uses the original API handlers on a disposable fixture.
// It is not a public serving mode or a tenant isolation certification. Its
// narrow fixture policies intentionally allow only the test's two tables.
func exercisePreparedHTTP(t *testing.T, owner *sql.DB, ownerURL, identifier string, config core.BaseAppConfig, manifest pgrolecheck.GrantManifest) {
	t.Helper()
	t.Setenv("RUXBASE_RLS_STRICT", "false")
	admin := core.NewBaseApp(core.BaseAppConfig{
		DatabaseURL: ownerURL, LogsDatabaseURL: ownerURL, DataDir: t.TempDir(),
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

	// Build expected grants from the reviewed test policy and declared object
	// names, not from observed runtime ACLs. Copy slices before modifying.
	manifest.Schemas = append([]pgrolecheck.GrantContract(nil), manifest.Schemas...)
	for i := range manifest.Schemas {
		schema := &manifest.Schemas[i]
		if schema.Schema != "public" {
			continue
		}
		schema.Grants = append([]pgrolecheck.Grant(nil), schema.Grants...)
		for _, target := range []struct {
			name        string
			permissions []string
		}{
			{"m01_fixture_users", []string{"SELECT"}},
			{"m01_fixture_items", []string{"SELECT", "INSERT", "UPDATE", "DELETE"}},
		} {
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
	raw, err := json.Marshal(map[string]pgrolecheck.GrantManifest{"main": manifest, "auxiliary": manifest})
	if err != nil {
		t.Fatal(err)
	}
	config.PreparedManifestJSON = string(raw)
	config.DataDir = t.TempDir()
	app := core.NewBaseApp(config)
	defer app.ClearBootstrap()
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("restricted HTTP fixture bootstrap: %v", err)
	}
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	// Use the real HTTP mux only behind a test-local allowlist. Never open
	// a production listener or publish administrative routes.
	const records = "/api/collections/m01_fixture_items/records"
	const login = "/api/collections/m01_fixture_users/auth-with-password"
	limited := http.NewServeMux()
	limited.Handle("POST "+login, mux)
	limited.Handle(records, mux)
	limited.Handle(records+"/{id}", mux)

	request := func(method, path, body, token string, expected int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", token)
		}
		res := httptest.NewRecorder()
		limited.ServeHTTP(res, req)
		if res.Code != expected {
			t.Fatalf("%s %s: expected %d, got %d", method, path, expected, res.Code)
		}
		return res
	}
	request(http.MethodGet, "/api/settings", "", "", http.StatusNotFound)
	guest := request(http.MethodGet, records, "", "", http.StatusOK)
	var guestList struct {
		TotalItems int `json:"totalItems"`
	}
	if err := json.Unmarshal(guest.Body.Bytes(), &guestList); err != nil || guestList.TotalItems != 0 {
		t.Fatal("guest list exposed authenticated fixture records")
	}
	request(http.MethodPost, login, `{"identity":"fixture@example.test","password":"incorrect"}`, "", http.StatusBadRequest)
	res := request(http.MethodPost, login, `{"identity":"fixture@example.test","password":"fixture-password-123"}`, "", http.StatusOK)
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatal("fixture authentication did not return a token")
	}
	res = request(http.MethodPost, records, `{"title":"fixture-created"}`, session.Token, http.StatusOK)
	var item struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &item); err != nil || item.ID == "" {
		t.Fatal("fixture create did not return a record")
	}
	path := records + "/" + item.ID
	request(http.MethodGet, path, "", session.Token, http.StatusOK)
	request(http.MethodGet, path, "", "", http.StatusNotFound)
	guest = request(http.MethodGet, records, "", "", http.StatusOK)
	if err := json.Unmarshal(guest.Body.Bytes(), &guestList); err != nil || guestList.TotalItems != 0 {
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
