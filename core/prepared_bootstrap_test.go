package core_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/internal/operatorprep"
	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/jackc/pgx/v5"
)

func TestPreparedBootstrapPostgreSQL(t *testing.T) {
	if os.Getenv(tests.TestDatabaseURLEnv) == "" {
		t.Skip("RUXBASE_TEST_URL not configured")
	}
	fixture, err := tests.NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(fixture.Name)

	owner, err := sql.Open("pgx", fixture.URL)
	if err != nil {
		t.Fatal("unable to open disposable owner connection")
	}
	defer owner.Close()
	var ownerRole string
	if err := owner.QueryRow("SELECT current_user").Scan(&ownerRole); err != nil {
		t.Fatal(err)
	}
	if err := operatorprep.Prepare(operatorprep.Config{
		DatabaseURL:  fixture.URL,
		ExpectedRole: ownerRole,
		DataDir:      t.TempDir(),
	}); err != nil {
		t.Fatal(err)
	}

	// All role and ACL changes below are scoped to this disposable fixture.
	role := fixture.Name + "_prepared"
	identifier := pgx.Identifier{role}.Sanitize()
	password := security.RandomStringWithAlphabet(32, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	create := "CREATE ROLE " + identifier + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "'"
	if _, err := owner.Exec(create); err != nil {
		t.Fatal("unable to create prepared fixture role")
	}
	defer func() {
		for _, query := range []string{
			"REVOKE SELECT ON public._migrations, public._params, public._collections FROM " + identifier,
			"REVOKE USAGE ON SCHEMA public, _aux FROM " + identifier,
			"DROP ROLE " + identifier,
		} {
			if _, err := owner.Exec(query); err != nil {
				t.Error("unable to clean up prepared fixture role")
			}
		}
	}()
	for _, query := range []string{
		"GRANT USAGE ON SCHEMA public, _aux TO " + identifier,
		"GRANT SELECT ON public._migrations, public._params, public._collections TO " + identifier,
	} {
		if _, err := owner.Exec(query); err != nil {
			t.Fatal("unable to prepare metadata grants")
		}
	}
	u, err := url.Parse(fixture.URL)
	if err != nil {
		t.Fatal("invalid fixture connection URL")
	}
	u.User = url.UserPassword(role, password)

	manifest := preparedFixtureManifest(t, owner, role)
	// Verify the expected fixture policy independently before exercising
	// either bootstrap rejection. Keep drift evidence local to the test.
	diagnostic, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal("unable to open fixture diagnostic connection")
	}
	defer diagnostic.Close()
	drift, err := manifest.Check(context.Background(), diagnostic)
	if err != nil {
		t.Fatalf("fixture manifest policy mismatch: %+v (%v)", drift, err)
	}
	encode := func(main, aux pgrolecheck.GrantManifest) string {
		t.Helper()
		raw, err := json.Marshal(map[string]pgrolecheck.GrantManifest{"main": main, "auxiliary": aux})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	config := core.BaseAppConfig{
		DatabaseURL:          u.String(),
		LogsDatabaseURL:      u.String(),
		DataDir:              t.TempDir(),
		PreparedRuntimeRole:  role,
		PreparedManifestJSON: encode(manifest, manifest),
	}
	app := core.NewBaseApp(config)
	defer app.ClearBootstrap()

	// Main is valid but auxiliary omits an expected schema. It must not
	// inherit main's admission decision even when both DSNs are identical.
	badAux := manifest
	badAux.Schemas = manifest.Schemas[:len(manifest.Schemas)-1]
	badConfig := config
	badConfig.PreparedManifestJSON = encode(manifest, badAux)
	rejected := core.NewBaseApp(badConfig)
	defer rejected.ClearBootstrap()
	if err := rejected.Bootstrap(); err == nil ||
		err.Error() != "prepared auxiliary database grant manifest rejected" || rejected.IsBootstrapped() {
		t.Fatalf("auxiliary manifest mismatch was not rejected: %v", err)
	}

	badConfig.PreparedManifestJSON = encode(badAux, manifest)
	rejectedMain := core.NewBaseApp(badConfig)
	defer rejectedMain.ClearBootstrap()
	if err := rejectedMain.Bootstrap(); err == nil ||
		err.Error() != "prepared main database grant manifest rejected" || rejectedMain.IsBootstrapped() {
		t.Fatalf("main manifest mismatch was not rejected: %v", err)
	}

	var count int
	if err := owner.QueryRow("SELECT count(*) FROM public._migrations").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	if !app.IsBootstrapped() {
		t.Fatal("prepared runtime did not initialize")
	}
	if _, err := app.FindCachedCollectionByNameOrId("_superusers"); err != nil {
		t.Fatal("prepared collection cache is missing")
	}
	for _, migrate := range []func() error{app.RunAllMigrations, app.RunAppMigrations, app.RunSystemMigrations} {
		if err := migrate(); !errors.Is(err, core.ErrPreparedBootstrapOnly) {
			t.Fatal("migration entrypoint not blocked")
		}
	}
	if err := apis.Serve(app, apis.ServeConfig{HttpAddr: "127.0.0.1:0"}); !errors.Is(err, core.ErrPreparedBootstrapOnly) {
		t.Fatalf("prepared-only profile admitted to serving: %v", err)
	}
	if err := app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
	var after int
	if err := owner.QueryRow("SELECT count(*) FROM public._migrations").Scan(&after); err != nil || count != after {
		t.Fatal("prepared bootstrap altered migration ledger")
	}

	// A later grant change must reject the next bootstrap despite an intact
	// migration history. Restore only this disposable fixture's grant.
	revokeMetadata := "REVOKE SELECT ON public._params FROM " + identifier
	if _, err := owner.Exec(revokeMetadata); err != nil {
		t.Fatal("unable to prepare disposable grant drift")
	}
	if err := app.Bootstrap(); err == nil ||
		err.Error() != "prepared main database grant manifest rejected" || app.IsBootstrapped() {
		t.Fatalf("missing metadata grant was not rejected: %v", err)
	}
	restoreMetadata := "GRANT SELECT ON public._params TO " + identifier
	if _, err := owner.Exec(restoreMetadata); err != nil {
		t.Fatal("unable to restore disposable fixture grant")
	}

	// A privileged owner and even a valid restricted login against incomplete
	// metadata must be rejected, and no bootstrap write may repair it.
	privileged := core.NewBaseApp(core.BaseAppConfig{
		DatabaseURL:         fixture.URL,
		LogsDatabaseURL:     fixture.URL,
		DataDir:             t.TempDir(),
		PreparedRuntimeRole: ownerRole,
	})
	defer privileged.ClearBootstrap()
	if err := privileged.Bootstrap(); err == nil || privileged.IsBootstrapped() {
		t.Fatal("owner admitted to prepared runtime")
	}
	if _, err := owner.Exec("DELETE FROM public._params WHERE id = 'settings'"); err != nil {
		t.Fatal(err)
	}
	if err := app.Bootstrap(); err == nil || app.IsBootstrapped() {
		t.Fatal("missing settings admitted to prepared runtime")
	}
	var exists bool
	if err := owner.QueryRow("SELECT EXISTS (SELECT 1 FROM public._params WHERE id = 'settings')").Scan(&exists); err != nil || exists {
		t.Fatal("prepared bootstrap recreated missing settings")
	}
}

// preparedFixtureManifest derives object names only for extension and built-in
// public routines in this controlled CI fixture; it never reads the runtime
// ACLs to decide what to approve. The fixture policy explicitly expects
// default PUBLIC EXECUTE on public routines, no rights on ruxbase_rls,
// and only the three metadata table reads plus schema usage. This is not a
// production manifest generator or a serving-role grant recommendation.
func preparedFixtureManifest(t *testing.T, owner *sql.DB, role string) pgrolecheck.GrantManifest {
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
SELECT pg_catalog.quote_ident(p.proname) || '(' ||
       pg_catalog.pg_get_function_identity_arguments(p.oid) || ')'
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname = 'public'
`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var signature string
		if err := rows.Scan(&signature); err != nil {
			t.Fatal(err)
		}
		main.Grants = append(main.Grants, pgrolecheck.Grant{Kind: "routine", Object: signature, Privilege: "EXECUTE"})
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
			{Role: role, Schema: "_aux", Extended: true, Grants: []pgrolecheck.Grant{
				{Kind: "schema", Object: "_aux", Privilege: "USAGE"},
			}},
			{Role: role, Schema: "ruxbase_rls", Extended: true, Grants: []pgrolecheck.Grant{}},
		},
	}
}
