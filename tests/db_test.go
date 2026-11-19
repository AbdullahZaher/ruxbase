package tests

import (
	"os"
	"slices"
	"testing"
)

func TestDatabaseURLHelpers(t *testing.T) {
	t.Parallel()

	name, err := databaseNameFromURL("postgres://u:p@localhost:5433/ruxbase_test?sslmode=disable")
	if err != nil || name != "ruxbase_test" {
		t.Fatalf("Expected ruxbase_test, got %q (%v)", name, err)
	}

	name, err = databaseNameFromURL("host=localhost dbname='ruxbase_test' user=u")
	if err != nil || name != "ruxbase_test" {
		t.Fatalf("Expected ruxbase_test, got %q (%v)", name, err)
	}

	if _, err := databaseNameFromURL("postgres://localhost:5433"); err == nil {
		t.Fatal("Expected missing database name error")
	}

	dsn, err := replaceDatabaseName("postgres://u:p@localhost:5433/ruxbase_test?sslmode=disable", "ruxbase_t_abc")
	if err != nil || dsn != "postgres://u:p@localhost:5433/ruxbase_t_abc?sslmode=disable" {
		t.Fatalf("Unexpected dsn %q (%v)", dsn, err)
	}
}

func TestDropTestDatabaseRefusesNonTestDatabases(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"ruxbase", "ruxbase_test", "ruxbase_try", "postgres", "ruxbase_t", "ruxbase_fx"} {
		if err := DropTestDatabase(name); err == nil {
			t.Fatalf("Expected %q to be refused", name)
		}
	}
}

func TestNewTestDatabase(t *testing.T) {
	if os.Getenv(TestDatabaseURLEnv) == "" {
		t.Skip("missing " + TestDatabaseURLEnv)
	}

	db, err := NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}

	admin, err := testAdminDB(os.Getenv(TestDatabaseURLEnv))
	if err != nil {
		t.Fatal(err)
	}

	listNames := func() []string {
		names := []string{}
		rows, err := admin.Query("SELECT datname FROM pg_database")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			rows.Scan(&name)
			names = append(names, name)
		}
		return names
	}

	if !slices.Contains(listNames(), db.Name) {
		t.Fatalf("Expected database %q to be created", db.Name)
	}

	if err := DropTestDatabase(db.Name); err != nil {
		t.Fatal(err)
	}

	if slices.Contains(listNames(), db.Name) {
		t.Fatalf("Expected database %q to be dropped", db.Name)
	}
}
