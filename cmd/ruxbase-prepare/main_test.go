package main

import "testing"

func TestPrepareRequiresExplicitConfirmation(t *testing.T) {
	t.Setenv("RUXBASE_PREPARE_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "not-an-operator-url")
	if err := run(nil); err == nil {
		t.Fatal("unconfirmed preparation accepted")
	}
	if err := run([]string{"--apply-system", "unexpected"}); err == nil {
		t.Fatal("unexpected arguments accepted")
	}
	if err := run([]string{"--apply-system"}); err == nil {
		t.Fatal("implicit database configuration accepted")
	}
}

func TestReviewedIdentities(t *testing.T) {
	env := map[string]string{"RUXBASE_PREPARE_ROLE": "migration"}
	get := func(key string) string { return env[key] }
	if contract, err := reviewedIdentities(false, get); err != nil || contract != nil {
		t.Fatal("legacy explicit operator configuration changed")
	}
	if _, err := reviewedIdentities(true, get); err == nil {
		t.Fatal("missing separate role names accepted")
	}
	env["RUXBASE_PREPARE_RUNTIME_ROLE"] = "runtime"
	env["RUXBASE_PREPARE_PROVISIONING_ROLE"] = "provisioner"
	env["RUXBASE_PREPARE_REPLICATION_ROLE"] = "wal"
	if contract, err := reviewedIdentities(true, get); err != nil || contract == nil || contract.Migration != "migration" || contract.Replication != "wal" {
		t.Fatal("reviewed role contract rejected")
	}
	if _, err := reviewedIdentities(false, get); err == nil {
		t.Fatal("separate role declarations silently ignored")
	}
	env["RUXBASE_PREPARE_RUNTIME_ROLE"] = "migration"
	if _, err := reviewedIdentities(true, get); err == nil {
		t.Fatal("shared runtime and migration identities accepted")
	}
}
