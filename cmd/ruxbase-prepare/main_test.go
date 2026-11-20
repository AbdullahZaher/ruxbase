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
