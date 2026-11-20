package operatorprep

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/jackc/pgx/v5"
)

func TestExplicitOperatorConfig(t *testing.T) {
	if err := (Config{}).Validate(); err == nil {
		t.Fatal("empty operator config accepted")
	}
	c := Config{DatabaseURL: "fixture", ExpectedRole: "operator", DataDir: "fixture"}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	c.ExpectedRole = " "
	if err := c.Validate(); err == nil {
		t.Fatal("blank expected role accepted")
	}
}

func TestPrepareDisposablePostgreSQL(t *testing.T) {
	if os.Getenv(tests.TestDatabaseURLEnv) == "" {
		t.Skip("RUXBASE_TEST_URL not configured")
	}
	fixture, err := tests.NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(fixture.Name)
	db, err := sql.Open("pgx", fixture.URL)
	if err != nil {
		t.Fatal("unable to open disposable database")
	}
	defer db.Close()

	var role string
	if err := db.QueryRow("SELECT current_user").Scan(&role); err != nil {
		t.Fatal(err)
	}

	c := Config{DatabaseURL: fixture.URL, ExpectedRole: role + "_mismatch", DataDir: t.TempDir()}
	if err := Prepare(c); err == nil {
		t.Fatal("mismatched operator accepted")
	}
	var absent bool
	if err := db.QueryRow("SELECT to_regclass('public._migrations') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("identity mismatch did not prevent migration initialization")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if status, err := CheckSystemHistory(ctx, fixture.URL, "", role); err == nil || len(status.Missing) == 0 {
		t.Fatalf("empty database passed history check: %+v (%v)", status, err)
	}
	if err := db.QueryRow("SELECT to_regclass('public._migrations') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("read-only history check created a migration table")
	}

	c.ExpectedRole = role
	// The existing test owner is deliberately administrative. The separate
	// migration mode must reject it before creating any migration ledger.
	separate := c
	separate.PreprovisionedRLSRoles = true
	if err := Prepare(separate); err == nil {
		t.Fatal("administrative owner admitted as separate migration principal")
	}
	if err := db.QueryRow("SELECT to_regclass('public._migrations') IS NULL").Scan(&absent); err != nil || !absent {
		t.Fatal("separate migration rejection modified the database")
	}
	if err := Prepare(c); err != nil {
		t.Fatal(err)
	}
	if status, err := CheckSystemHistory(ctx, fixture.URL, "", role); err != nil {
		t.Fatalf("prepared system history rejected: %+v (%v)", status, err)
	}
	if _, err := CheckSystemHistory(ctx, fixture.URL, "", role+"_mismatch"); err == nil {
		t.Fatal("wrong history inspection identity accepted")
	}
	// A completed main ledger must not mask a missing auxiliary database.
	aux, err := tests.NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(aux.Name)
	status, err := CheckSystemHistory(ctx, fixture.URL, aux.URL, role)
	if err == nil || len(status.Missing) != 0 || len(status.Conditional) != 1 ||
		status.Conditional[0] != "1640988000_aux_init.go" {
		t.Fatalf("missing auxiliary table was not rejected: %+v (%v)", status, err)
	}
	var before, after int
	if err := db.QueryRow("SELECT count(*) FROM public._migrations").Scan(&before); err != nil || before == 0 {
		t.Fatal("system migration history missing")
	}

	if err := Prepare(c); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT count(*) FROM public._migrations").Scan(&after); err != nil || before != after {
		t.Fatalf("unexpected migration history change: %d -> %d (%v)", before, after, err)
	}
	verifySeparateMigration(t)
}

// verifySeparateMigration uses a new disposable database and generated
// migration login. The first fixture has already provisioned the shared
// RLS roles. No production database or persistent role is modified.
func verifySeparateMigration(t *testing.T) {
	t.Helper()
	fixture, err := tests.NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	owner, err := sql.Open("pgx", fixture.URL)
	if err != nil {
		_ = tests.DropTestDatabase(fixture.Name)
		t.Fatal("unable to open disposable provisioner")
	}
	role := fixture.Name + "_migration"
	identifier := pgx.Identifier{role}.Sanitize()
	password := security.RandomStringWithAlphabet(32, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	created := false
	runtimeRole := fixture.Name + "_runtime"
	runtimeIdentifier := pgx.Identifier{runtimeRole}.Sanitize()
	runtimeCreated := false
	defer func() {
		_ = owner.Close()
		if err := tests.DropTestDatabase(fixture.Name); err != nil {
			t.Errorf("unable to remove migration fixture database: %v", err)
		}
		if created {
			maintenance, err := url.Parse(fixture.URL)
			if err != nil {
				t.Error("invalid fixture maintenance URL")
				return
			}
			maintenance.Path = "/postgres"
			db, err := sql.Open("pgx", maintenance.String())
			if err != nil {
				t.Error("unable to open fixture cleanup connection")
				return
			}
			defer db.Close()
			if runtimeCreated {
				dropRuntime := "DROP ROLE " + runtimeIdentifier
				if _, err := db.Exec(dropRuntime); err != nil {
					t.Errorf("unable to remove disposable runtime role: %v", err)
				}
			}
			drop := "DROP ROLE " + identifier
			if _, err := db.Exec(drop); err != nil {
				t.Errorf("unable to remove migration fixture role: %v", err)
			}
		}
	}()
	create := "CREATE ROLE " + identifier + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "'"
	if _, err := owner.Exec(create); err != nil {
		t.Fatal("unable to create disposable migration role")
	}
	created = true
	runtimePassword := security.RandomStringWithAlphabet(32, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	createRuntime := "CREATE ROLE " + runtimeIdentifier + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + runtimePassword + "'"
	if _, err := owner.Exec(createRuntime); err != nil {
		t.Fatal("unable to create disposable runtime role")
	}
	runtimeCreated = true
	var provisioner string
	if err := owner.QueryRow("SELECT current_user").Scan(&provisioner); err != nil {
		t.Fatal(err)
	}
	identities := pgrolecheck.IdentityContract{Runtime: runtimeRole, Migration: role, Provisioning: provisioner}
	if raw := os.Getenv("RUXBASE_TEST_WAL_URL"); raw != "" {
		wal, err := url.Parse(raw)
		if err != nil || wal.User == nil || wal.User.Username() != "ruxbase_ci_wal" {
			t.Fatal("invalid disposable WAL fixture identity")
		}
		identities.Replication = wal.User.Username()
	}
	// The provisioner grants schema preparation authority only on this
	// new database, not cluster-wide role or database creation authority.
	for _, statement := range []string{
		"GRANT CONNECT, CREATE ON DATABASE " + pgx.Identifier{fixture.Name}.Sanitize() + " TO " + identifier,
		"GRANT USAGE, CREATE ON SCHEMA public TO " + identifier,
		"GRANT ruxbase_anon, ruxbase_authenticated, ruxbase_api TO " + identifier + " WITH INHERIT FALSE, SET TRUE",
	} {
		if _, err := owner.Exec(statement); err != nil {
			t.Fatal("unable to provision disposable migration role")
		}
	}
	u, err := url.Parse(fixture.URL)
	if err != nil {
		t.Fatal("invalid migration fixture URL")
	}
	u.User = url.UserPassword(role, password)
	config := Config{
		DatabaseURL:            u.String(),
		ExpectedRole:           role,
		DataDir:                t.TempDir(),
		PreprovisionedRLSRoles: true,
		IdentityContract:       &identities,
	}
	if err := Prepare(config); err != nil {
		t.Fatalf("preprovisioned migration failed: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if status, err := CheckSystemHistory(ctx, u.String(), "", role); err != nil {
		t.Fatalf("separate migration history invalid: %+v (%v)", status, err)
	}
	// PostgreSQL may need a short interval to observe orderly connection
	// closure. Do not force-terminate another principal's sessions.
	deadline := time.Now().Add(5 * time.Second)
	for {
		var active int
		if err := owner.QueryRow("SELECT count(*) FROM pg_catalog.pg_stat_activity WHERE datname = current_database() AND usename = $1", role).Scan(&active); err != nil {
			t.Fatal(err)
		}
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("offline operator left %d migration sessions", active)
		}
		time.Sleep(25 * time.Millisecond)
	}
	var migrationOwned bool
	if err := owner.QueryRow(`
SELECT pg_catalog.pg_get_userbyid(c.relowner) = $1
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = 'public' AND c.relname = '_migrations'
`, role).Scan(&migrationOwned); err != nil || !migrationOwned {
		t.Fatal("migration history was not created by the separate identity")
	}
	if err := Prepare(config); err != nil {
		t.Fatalf("separate migration replay failed: %v", err)
	}

	// Admit the restricted metadata runtime against the same prepared
	// database. Grants are given by the respective fixture object owners,
	// not by the runtime. No public serving is enabled.
	migration, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal("unable to open disposable migration connection")
	}
	defer migration.Close()
	for _, statement := range []string{
		"GRANT USAGE ON SCHEMA _aux TO " + runtimeIdentifier,
		"GRANT SELECT ON public._migrations, public._params, public._collections TO " + runtimeIdentifier,
	} {
		if _, err := migration.Exec(statement); err != nil {
			t.Fatal("unable to prepare disposable runtime metadata grants")
		}
	}
	runtimeURL := *u
	runtimeURL.User = url.UserPassword(runtimeRole, runtimePassword)
	runtime, err := sql.Open("pgx", runtimeURL.String())
	if err != nil {
		t.Fatal("unable to open disposable runtime connection")
	}
	defer runtime.Close()
	checkCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	if err := identities.CheckSeparation(checkCtx, runtime, runtimeRole); err != nil {
		t.Fatal(err)
	}
	if _, err := identities.AdmitRuntime(checkCtx, runtime); err != nil {
		t.Fatal(err)
	}
	if err := identities.CheckSeparation(checkCtx, runtime, role); err == nil {
		t.Fatal("runtime accepted as migration principal")
	}
	missing := identities
	missing.Migration = fixture.Name + "_missing"
	if err := missing.CheckSeparation(checkCtx, runtime, runtimeRole); err == nil {
		t.Fatal("absent declared migration identity accepted")
	}
	app := core.NewBaseApp(core.BaseAppConfig{
		DatabaseURL:         runtimeURL.String(),
		LogsDatabaseURL:     runtimeURL.String(),
		DataDir:             t.TempDir(),
		PreparedRuntimeRole: runtimeRole,
		RealtimeSource:      "hooks",
	})
	defer app.ClearBootstrap()
	if err := app.Bootstrap(); err != nil {
		t.Fatalf("runtime rejected separately migrated disposable database: %v", err)
	}
	if err := app.ClearBootstrap(); err != nil {
		t.Fatal(err)
	}
}
