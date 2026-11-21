package core_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

func TestScopedTransactionRoleAndTenantIsolation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatal(err)
	}

	scope := core.TransactionScope{
		Principal: user,
		TenantID:  "tenant_abc_123",
		Rule:      core.RLSRuleList,
		Claims: map[string]string{
			"ruxbase.custom_claim": "verified_value",
		},
	}

	// 1. Execute inside scoped transaction
	err = app.RunInScopedTransaction(scope, func(txApp core.App) error {
		var currentUser string
		if err := txApp.DB().NewQuery("SELECT current_user").Row(&currentUser); err != nil {
			return err
		}
		if currentUser != core.RLSAuthenticatedRole {
			return fmt.Errorf("expected current_user %q, got %q", core.RLSAuthenticatedRole, currentUser)
		}

		var token string
		if err := txApp.DB().NewQuery("SELECT current_setting('ruxbase.token', true)").Row(&token); err != nil {
			return err
		}
		if token == "" {
			return fmt.Errorf("expected non-empty ruxbase.token in scoped transaction")
		}

		var rule string
		if err := txApp.DB().NewQuery("SELECT current_setting('ruxbase.rule', true)").Row(&rule); err != nil {
			return err
		}
		if rule != core.RLSRuleList {
			return fmt.Errorf("expected ruxbase.rule %q, got %q", core.RLSRuleList, rule)
		}

		var tenant string
		if err := txApp.DB().NewQuery("SELECT current_setting('ruxbase.tenant_id', true)").Row(&tenant); err != nil {
			return err
		}
		if tenant != "tenant_abc_123" {
			return fmt.Errorf("expected ruxbase.tenant_id %q, got %q", "tenant_abc_123", tenant)
		}

		var custom string
		if err := txApp.DB().NewQuery("SELECT current_setting('ruxbase.custom_claim', true)").Row(&custom); err != nil {
			return err
		}
		if custom != "verified_value" {
			return fmt.Errorf("expected ruxbase.custom_claim %q, got %q", "verified_value", custom)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("scoped transaction failed: %v", err)
	}

	// 2. Verify connection pool hygiene immediately after transaction commits
	var poolUser string
	if err := app.DB().NewQuery("SELECT current_user").Row(&poolUser); err != nil {
		t.Fatal(err)
	}
	if poolUser == core.RLSAuthenticatedRole {
		t.Fatalf("connection pool leaked role %q after scoped transaction", poolUser)
	}

	var poolTenant string
	_ = app.DB().NewQuery("SELECT current_setting('ruxbase.tenant_id', true)").Row(&poolTenant)
	if poolTenant != "" {
		t.Fatalf("connection pool leaked ruxbase.tenant_id %q", poolTenant)
	}

	var poolToken string
	_ = app.DB().NewQuery("SELECT current_setting('ruxbase.token', true)").Row(&poolToken)
	if poolToken != "" {
		t.Fatalf("connection pool leaked ruxbase.token %q", poolToken)
	}

	var poolClaim string
	_ = app.DB().NewQuery("SELECT current_setting('ruxbase.custom_claim', true)").Row(&poolClaim)
	if poolClaim != "" {
		t.Fatalf("connection pool leaked ruxbase.custom_claim %q", poolClaim)
	}
}

func TestScopedTransactionRollbackAndCancellationIsolation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	col, err := app.FindCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}

	// 1. Verify rollback cleans up rows and session context
	rollbackScope := core.TransactionScope{
		TenantID: "tenant_failed_tx",
	}

	expectedErr := fmt.Errorf("intentional_scope_failure")
	err = app.RunInScopedTransaction(rollbackScope, func(txApp core.App) error {
		rec := core.NewRecord(col)
		rec.Set("text", "uncommitted_scope_text")
		if err := txApp.Save(rec); err != nil {
			return err
		}
		return expectedErr
	})
	if err == nil || !strings.Contains(err.Error(), "intentional_scope_failure") {
		t.Fatalf("expected intentional failure, got: %v", err)
	}

	// Verify uncommitted record does not exist
	records, err := app.FindAllRecords("demo1", dbx.HashExp{"text": "uncommitted_scope_text"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected 0 records after rollback, got %d", len(records))
	}

	// Verify tenant setting is not leaked
	var poolTenant string
	_ = app.DB().NewQuery("SELECT current_setting('ruxbase.tenant_id', true)").Row(&poolTenant)
	if poolTenant != "" {
		t.Fatalf("connection pool leaked ruxbase.tenant_id %q after rollback", poolTenant)
	}

	// 2. Verify cancellation context aborts cleanly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel context

	err = app.RunInScopedTransactionWithContext(ctx, rollbackScope, func(txApp core.App) error {
		t.Fatal("transaction body should not execute when context is pre-cancelled")
		return nil
	})
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled error, got: %v", err)
	}
}

func TestScopedTransactionElevationProtection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	// 1. Attempt role escalation to postgres
	err = app.RunInScopedTransaction(core.TransactionScope{Role: "postgres"}, func(txApp core.App) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "unsafe role escalation") {
		t.Fatalf("expected unsafe role escalation error for postgres, got: %v", err)
	}

	// 2. Attempt role escalation to admin
	err = app.RunInScopedTransaction(core.TransactionScope{Role: "admin"}, func(txApp core.App) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "unsafe role escalation") {
		t.Fatalf("expected unsafe role escalation error for admin, got: %v", err)
	}

	// 3. Superuser principal rejected from RLS scope
	su, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatal(err)
	}

	err = app.RunInScopedTransaction(core.TransactionScope{Principal: su}, func(txApp core.App) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "superusers bypass collection RLS") {
		t.Fatalf("expected superusers bypass error, got: %v", err)
	}
}

func TestScopedTransactionReadOnly(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	col, err := app.FindCollectionByNameOrId("demo1")
	if err != nil {
		t.Fatal(err)
	}

	scope := core.TransactionScope{
		Role:     core.RLSAnonRole,
		ReadOnly: true,
	}

	err = app.RunInScopedTransaction(scope, func(txApp core.App) error {
		rec := core.NewRecord(col)
		rec.Set("text", "read_only_violation")
		return txApp.Save(rec)
	})
	if err == nil {
		t.Fatal("expected error when attempting write inside read-only transaction")
	}
	if !strings.Contains(err.Error(), "read-only") && !strings.Contains(err.Error(), "25006") {
		t.Fatalf("expected read-only transaction violation error, got: %v", err)
	}
}

func TestScopedTransactionConcurrentAbuse(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	users, err := app.FindAllRecords("users", dbx.NewExp("1=1"))
	if err != nil || len(users) < 2 {
		t.Fatalf("expected at least 2 users, got %d", len(users))
	}

	const concurrency = 20
	const iterations = 10

	var wg sync.WaitGroup
	var errorCount int64

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		tenantID := fmt.Sprintf("tenant_worker_%d", i)
		user := users[i%len(users)]

		go func(tid string, u *core.Record) {
			defer wg.Done()

			scope := core.TransactionScope{
				Principal: u,
				TenantID:  tid,
				Claims: map[string]string{
					"ruxbase.worker_id": tid,
				},
			}

			for j := 0; j < iterations; j++ {
				err := app.RunInScopedTransaction(scope, func(txApp core.App) error {
					var readTenant string
					if err := txApp.DB().NewQuery("SELECT current_setting('ruxbase.tenant_id', true)").Row(&readTenant); err != nil {
						return err
					}
					if readTenant != tid {
						return fmt.Errorf("tenant isolation breached: expected %q, got %q", tid, readTenant)
					}

					var readWorker string
					if err := txApp.DB().NewQuery("SELECT current_setting('ruxbase.worker_id', true)").Row(&readWorker); err != nil {
						return err
					}
					if readWorker != tid {
						return fmt.Errorf("claim isolation breached: expected %q, got %q", tid, readWorker)
					}

					var readUser string
					if err := txApp.DB().NewQuery("SELECT current_user").Row(&readUser); err != nil {
						return err
					}
					if readUser != core.RLSAuthenticatedRole {
						return fmt.Errorf("role isolation breached: expected %q, got %q", core.RLSAuthenticatedRole, readUser)
					}

					// Yield briefly to exercise pool interleaving
					time.Sleep(2 * time.Millisecond)

					return nil
				})

				if err != nil {
					atomic.AddInt64(&errorCount, 1)
					t.Errorf("worker %s iteration %d failed: %v", tid, j, err)
					return
				}
			}
		}(tenantID, user)
	}

	wg.Wait()

	if errorCount > 0 {
		t.Fatalf("concurrent scoped transaction test encountered %d errors", errorCount)
	}

	// Final pool check
	var poolTenant string
	_ = app.DB().NewQuery("SELECT current_setting('ruxbase.tenant_id', true)").Row(&poolTenant)
	if poolTenant != "" {
		t.Fatalf("final pool connection leaked tenant %q", poolTenant)
	}
}
