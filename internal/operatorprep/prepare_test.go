package operatorprep

import (
	"database/sql"
	"os"
	"testing"

	"github.com/AbdullahZaher/ruxbase/tests"
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

	c.ExpectedRole = role
	if err := Prepare(c); err != nil {
		t.Fatal(err)
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
}
