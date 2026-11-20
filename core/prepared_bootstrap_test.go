package core_test

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/internal/operatorprep"
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

	app := core.NewBaseApp(core.BaseAppConfig{
		DatabaseURL:         u.String(),
		LogsDatabaseURL:     u.String(),
		DataDir:             t.TempDir(),
		PreparedRuntimeRole: role,
	})
	defer app.ClearBootstrap()

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
