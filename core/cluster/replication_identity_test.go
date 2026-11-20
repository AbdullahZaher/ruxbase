package cluster

import "testing"

func TestDedicatedReplicationConfiguration(t *testing.T) {
	const primary = "postgres://runtime:fixture@localhost:5432/ruxbase_test?sslmode=disable"
	const replica = "postgres://wal:fixture@localhost:5432/ruxbase_test?sslmode=disable"
	if (DedicatedReplication{}).Enabled() {
		t.Fatal("empty dedicated configuration changed legacy behavior")
	}
	valid := DedicatedReplication{DatabaseURL: replica, Role: "wal"}
	if !valid.Enabled() {
		t.Fatal("explicit configuration not enabled")
	}
	if _, err := valid.validate(primary); err != nil {
		t.Fatal(err)
	}
	for _, config := range []DedicatedReplication{
		{Role: "wal"},
		{DatabaseURL: replica},
		{DatabaseURL: replica, Role: "other"},
		{DatabaseURL: primary, Role: "runtime"},
		{DatabaseURL: "postgres://wal:fixture@localhost:5432/other?sslmode=disable", Role: "wal"},
		{DatabaseURL: "postgres://wal:fixture@localhost:5433/ruxbase_test?sslmode=disable", Role: "wal"},
		{DatabaseURL: "postgres://wal:fixture@other:5432/ruxbase_test?sslmode=disable", Role: "wal"},
		{DatabaseURL: replica + "&replication=database", Role: "wal"},
	} {
		if !config.Enabled() {
			t.Fatal("partial or invalid configuration treated as legacy")
		}
		if _, err := config.validate(primary); err == nil {
			t.Fatal("invalid dedicated replication configuration accepted")
		}
	}
}
