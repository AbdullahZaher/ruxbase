package pgrolecheck

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/tests"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestReportFindings(t *testing.T) {
	clean := Report{SessionRole: "runtime", CurrentRole: "runtime"}
	if got := clean.Findings(); len(got) != 0 {
		t.Fatalf("unexpected findings: %v", got)
	}

	report := Report{
		SessionRole:      "login",
		CurrentRole:      "other",
		PrivilegedMember: true,
		DatabaseOwner:    true,
		DatabaseCreate:   true,
		SchemaOwner:      true,
		SchemaCreate:     true,
		RelationOwner:    true,
		RoutineOwner:     true,
	}
	want := []string{
		"session_identity_mismatch", "privileged_role_membership",
		"database_ownership", "database_create", "schema_ownership",
		"schema_create", "relation_ownership", "routine_ownership",
	}
	if got := report.Findings(); !reflect.DeepEqual(got, want) {
		t.Fatalf("findings = %v, want %v", got, want)
	}
	if got := (Report{}).Findings(); len(got) != 1 || got[0] != "session_identity_mismatch" {
		t.Fatalf("empty report must not pass: %v", got)
	}
}

func TestInspectRejectsMissingConnection(t *testing.T) {
	if _, err := Inspect(context.Background(), nil); err == nil {
		t.Fatal("missing connection accepted")
	}
}

// CI config intentionally grants the existing test role provisioning
// privileges. The screen must flag that role, without changing its grants.
func TestInspectPostgreSQL(t *testing.T) {
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
		t.Fatal("unable to open test connection")
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	report, err := Inspect(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if report.CurrentRole == "" || report.CurrentRole != report.SessionRole {
		t.Fatal("expected a direct test identity")
	}
	if !report.DatabaseOwner {
		t.Fatalf("expected the disposable database owner to be flagged: %+v", report)
	}
	if len(report.Findings()) == 0 {
		t.Fatal("existing owner role must fail the restricted identity screen")
	}

	cancelled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := Inspect(cancelled, db); err == nil {
		t.Fatal("cancelled inspection unexpectedly succeeded")
	}
}
