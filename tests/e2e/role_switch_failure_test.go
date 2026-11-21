package e2e_test

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestRoleSwitch_UnsafeEscalationRejection(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tenant, err := testApp.CreateTenant("Sec Org", "sec-org")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// 1. Unsafe role escalation attempts must be rejected immediately
	forbiddenRoles := []string{"postgres", "admin", "superuser", "pg_superuser", "root_admin"}

	for _, forbidden := range forbiddenRoles {
		err := testApp.RunInScopedTransaction(core.TransactionScope{
			Role:     forbidden,
			TenantID: tenant.Id,
		}, func(txApp core.App) error {
			return nil
		})

		if err == nil {
			t.Errorf("Expected role escalation to %q to be forbidden, got nil", forbidden)
		}
		if !strings.Contains(err.Error(), "unsafe role escalation") {
			t.Errorf("Expected unsafe role escalation error for %q, got: %v", forbidden, err)
		}
	}
}

func TestRoleSwitch_NoLeakageOnTransactionFailure(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tenant, err := testApp.CreateTenant("Leak Org", "leak-org")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// 1. Run a scoped transaction that fails midway
	_ = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tenant.Id,
		Claims:   map[string]string{"ruxbase.custom_claim": "secret_data"},
	}, func(txApp core.App) error {
		// Verify parameters are active inside transaction
		var activeTenant string
		_ = txApp.DB().NewQuery("SELECT current_setting('ruxbase.tenant_id', true)").Row(&activeTenant)
		if activeTenant != tenant.Id {
			return fmt.Errorf("expected inside transaction tenant %s, got %s", tenant.Id, activeTenant)
		}
		// Return deliberate failure to trigger rollback
		return errors.New("simulated fatal transaction abort")
	})

	// 2. Query pool connection outside transaction: ensure NO settings leaked!
	var poolTenant, poolClaim string
	_ = testApp.DB().NewQuery("SELECT current_setting('ruxbase.tenant_id', true)").Row(&poolTenant)
	_ = testApp.DB().NewQuery("SELECT current_setting('ruxbase.custom_claim', true)").Row(&poolClaim)

	if poolTenant != "" {
		t.Fatalf("Connection pool leaked ruxbase.tenant_id: %q", poolTenant)
	}
	if poolClaim != "" {
		t.Fatalf("Connection pool leaked custom claim: %q", poolClaim)
	}
}

func TestRevocationUnderConcurrency_InstantFailureAtT0(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	usersColl, err := testApp.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users: %v", err)
	}

	user := core.NewRecord(usersColl)
	user.SetEmail("concurrent_victim@test.com")
	user.SetPassword("1234567890")
	if err := testApp.Save(user); err != nil {
		t.Fatalf("Failed to save user: %v", err)
	}

	tenant, err := testApp.CreateTenant("Revoke Corp", "revoke-corp")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	_, err = testApp.AddTenantMember(tenant.Id, user.Id, core.RoleMember)
	if err != nil {
		t.Fatalf("Failed to add tenant member: %v", err)
	}

	// Issue token
	tokenStr, err := testApp.GenerateTenantAuthToken(user, tenant.Id, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Verify token works
	_, _, err = testApp.VerifyTenantAuthToken(tokenStr)
	if err != nil {
		t.Fatalf("Token should be valid: %v", err)
	}

	// Rotate tokenKey to simulate session revocation
	if err := testApp.RevokeUserSession(user); err != nil {
		t.Fatalf("Failed to revoke session: %v", err)
	}

	// Concurrently assert that 10 parallel requests are all rejected immediately
	concurrency := 10
	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, err := testApp.VerifyTenantAuthToken(tokenStr)
			if err != nil {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if errCount != concurrency {
		t.Fatalf("Expected all %d concurrent requests to be rejected after revocation, got %d rejected", concurrency, errCount)
	}
}
