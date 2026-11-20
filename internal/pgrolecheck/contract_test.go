package pgrolecheck

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestIdentityContract(t *testing.T) {
	valid := IdentityContract{Runtime: "runtime", Migration: "migration", Provisioning: "provisioning"}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []IdentityContract{
		{},
		{Runtime: "runtime", Migration: "runtime", Provisioning: "provisioning"},
		{Runtime: "runtime", Migration: "migration", Provisioning: "runtime"},
		{Runtime: "runtime", Migration: "migration", Provisioning: "migration"},
		{Runtime: "runtime", Migration: "migration", Provisioning: "provisioning", Replication: "runtime"},
		{Runtime: " runtime", Migration: "migration", Provisioning: "provisioning"},
		{Runtime: "runtime", Migration: "migration", Provisioning: "bad\x00role"},
	}
	for i, contract := range cases {
		if err := contract.Validate(); err == nil {
			t.Fatalf("accepted invalid contract %d", i)
		}
	}
	valid.Replication = "replication"
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := valid.AdmitRuntime(context.Background(), nil); err == nil {
		t.Fatal("missing connection admitted")
	}
}

func TestRestrictedIdentityAdmissionPostgreSQL(t *testing.T) {
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	role := fixture.Name + "_runtime"
	identifier := pgx.Identifier{role}.Sanitize()
	password := security.RandomStringWithAlphabet(32, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")
	// Test-only role; generated password contains strictly alphanumeric bytes.
	statement := "CREATE ROLE " + identifier + " LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS PASSWORD '" + password + "'"
	_, err = owner.ExecContext(ctx, statement)
	if err != nil {
		t.Fatal("unable to create disposable restricted role")
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := owner.ExecContext(cleanupCtx, "DROP ROLE " + identifier); err != nil {
			t.Error("unable to remove disposable restricted role")
		}
	}()

	u, err := url.Parse(fixture.URL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
		t.Fatal("test fixture must provide a PostgreSQL URL")
	}
	u.User = url.UserPassword(role, password)
	runtime, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal("unable to open restricted connection")
	}
	defer runtime.Close()

	contract := IdentityContract{
		Runtime: role, Migration: fixture.Name + "_migration", Provisioning: fixture.Name + "_provisioning",
	}
	report, err := contract.AdmitRuntime(ctx, runtime)
	if err != nil {
		t.Fatalf("restricted identity rejected: %v; findings: %v", err, report.Findings())
	}
	if report.SessionRole != role || report.CurrentRole != role {
		t.Fatal("unexpected admitted identity")
	}
	if _, err := contract.AdmitRuntime(ctx, owner); err == nil {
		t.Fatal("owner connection admitted as declared restricted runtime")
	}
	contract.Runtime = fixture.Name + "_different"
	if _, err := contract.AdmitRuntime(ctx, runtime); err == nil {
		t.Fatal("mismatched restricted identity admitted")
	}
}
