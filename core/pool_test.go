package core_test

import (
	"errors"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestPoolResetIdle(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	// Initial query works
	record, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil || record == nil {
		t.Fatalf("expected initial superuser query to succeed: %v", err)
	}

	// Reset DB pool while idle
	if err := app.ResetDBPool(); err != nil {
		t.Fatalf("unexpected error resetting pool: %v", err)
	}

	// Query still works cleanly after pool reset
	recordAfter, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil || recordAfter == nil {
		t.Fatalf("expected superuser query to succeed after pool reset: %v", err)
	}
}

func TestPoolResetInsideTransaction(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	err := app.RunInTransaction(func(txApp core.App) error {
		resetErr := txApp.ResetDBPool()
		if !errors.Is(resetErr, core.ErrTransactionalPoolReset) {
			t.Fatalf("expected ErrTransactionalPoolReset inside transaction, got %v", resetErr)
		}

		closeErr := txApp.CloseDBPool()
		if !errors.Is(closeErr, core.ErrTransactionalPoolReset) {
			t.Fatalf("expected ErrTransactionalPoolReset on close inside transaction, got %v", closeErr)
		}

		return nil
	})

	if err != nil {
		t.Fatalf("unexpected transaction error: %v", err)
	}
}
