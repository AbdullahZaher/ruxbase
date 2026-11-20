package pgrolecheck

import (
	"context"
	"database/sql"
	"net/url"
	"os"
	"reflect"
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
		if _, err := owner.ExecContext(cleanupCtx, "DROP ROLE "+identifier); err != nil {
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
	// Verify effective grants in an isolated schema belonging to the fixture.
	// The schema is removed before the temporary runtime login is dropped.
	if _, err := owner.ExecContext(ctx, "CREATE SCHEMA ruxbase_grant_contract"); err != nil {
		t.Fatal("unable to create grant fixture schema")
	}
	defer func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 15*time.Second)
		defer stop()
		if _, err := owner.ExecContext(cleanupCtx, "DROP SCHEMA ruxbase_grant_contract CASCADE"); err != nil {
			t.Error("unable to clean up grant fixture schema")
		}
	}()
	for _, query := range []string{
		"CREATE TABLE ruxbase_grant_contract.items (id integer PRIMARY KEY)",
		"CREATE SEQUENCE ruxbase_grant_contract.item_seq",
		"GRANT USAGE ON SCHEMA ruxbase_grant_contract TO " + identifier,
		"GRANT SELECT ON ruxbase_grant_contract.items TO " + identifier,
		"GRANT USAGE ON SEQUENCE ruxbase_grant_contract.item_seq TO " + identifier,
	} {
		if _, err := owner.ExecContext(ctx, query); err != nil {
			t.Fatal("unable to prepare scoped grant fixture")
		}
	}
	grants := GrantContract{
		Role:   role,
		Schema: "ruxbase_grant_contract",
		Grants: []Grant{
			{"schema", "ruxbase_grant_contract", "USAGE"},
			{"relation", "items", "SELECT"},
			{"sequence", "item_seq", "USAGE"},
		},
	}
	if drift, err := grants.Check(ctx, runtime); err != nil {
		t.Fatalf("approved scoped grants rejected: %v; drift: %+v", err, drift)
	}
	// Change only the expected contract to exercise drift rejection without
	// introducing extra database privileges.
	grants.Grants = []Grant{
		{"schema", "ruxbase_grant_contract", "USAGE"},
		{"relation", "items", "INSERT"},
		{"sequence", "item_seq", "USAGE"},
	}
	drift, err := grants.Check(ctx, runtime)
	if err == nil || !reflect.DeepEqual(drift.Missing, []Grant{{"relation", "items", "INSERT"}}) ||
		!reflect.DeepEqual(drift.Unexpected, []Grant{{"relation", "items", "SELECT"}}) {
		t.Fatalf("expected exact grant drift, got %+v (%v)", drift, err)
	}

	contract.Runtime = fixture.Name + "_different"
	if _, err := contract.AdmitRuntime(ctx, runtime); err == nil {
		t.Fatal("mismatched restricted identity admitted")
	}
}
