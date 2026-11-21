package e2e_test

import (
	"errors"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestServiceAccounts_LifecycleAndKeyRotation(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	// 1. Create service account
	sa, rawKey1, err := testApp.CreateServiceAccount(
		"CI Pipeline Worker",
		"",
		"Automated CI deployment worker",
		[]string{"records.read", "records.create"},
		[]string{"*"},
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create service account: %v", err)
	}
	if sa.Name != "CI Pipeline Worker" {
		t.Errorf("Expected name 'CI Pipeline Worker', got %s", sa.Name)
	}
	if sa.ApiKeyPrefix == "" || len(sa.ApiKeyPrefix) != 8 {
		t.Errorf("Expected 8-character prefix, got %s", sa.ApiKeyPrefix)
	}

	// 2. Authenticate with initial key
	sp, err := testApp.AuthenticateServiceAccount(rawKey1, "")
	if err != nil {
		t.Fatalf("Failed to authenticate with valid key: %v", err)
	}
	if sp.Id() != sa.Id {
		t.Errorf("Expected principal ID %s, got %s", sa.Id, sp.Id())
	}
	if sp.Type() != core.PrincipalTypeService {
		t.Errorf("Expected type 'service', got %s", sp.Type())
	}
	if !sp.HasPermission("records.read") || !sp.HasPermission("records.create") {
		t.Errorf("Principal missing expected permissions: %v", sp.Permissions())
	}

	// 3. Rotate API key
	rawKey2, err := testApp.RotateServiceAccountKey(sa.Id)
	if err != nil {
		t.Fatalf("Failed to rotate key: %v", err)
	}
	if rawKey2 == rawKey1 {
		t.Fatalf("Rotated key must be distinct from original key")
	}

	// Old key must be immediately rejected at T=0
	_, err = testApp.AuthenticateServiceAccount(rawKey1, "")
	if !errors.Is(err, core.ErrInvalidApiKey) {
		t.Errorf("Expected ErrInvalidApiKey for rotated old key, got: %v", err)
	}

	// New key must succeed
	sp2, err := testApp.AuthenticateServiceAccount(rawKey2, "")
	if err != nil {
		t.Fatalf("Failed to authenticate with new rotated key: %v", err)
	}
	if sp2.Id() != sa.Id {
		t.Errorf("Expected same service account ID after rotation")
	}

	// 4. Revocation
	if err := testApp.RevokeServiceAccount(sa.Id); err != nil {
		t.Fatalf("Failed to revoke service account: %v", err)
	}

	// New key must now fail with ErrServiceAccountRevoked
	_, err = testApp.AuthenticateServiceAccount(rawKey2, "")
	if !errors.Is(err, core.ErrServiceAccountRevoked) {
		t.Errorf("Expected ErrServiceAccountRevoked for revoked service account, got: %v", err)
	}
}

func TestServiceAccounts_ExpirationAndScopes(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	// 1. Create an already expired service account
	pastTime := time.Now().Add(-1 * time.Hour)
	_, expiredKey, err := testApp.CreateServiceAccount(
		"Expired Worker",
		"",
		"Worker past TTL",
		[]string{"records.read"},
		[]string{"*"},
		&pastTime,
	)
	if err != nil {
		t.Fatalf("Failed to create expired account: %v", err)
	}

	_, err = testApp.AuthenticateServiceAccount(expiredKey, "")
	if !errors.Is(err, core.ErrServiceAccountExpired) {
		t.Errorf("Expected ErrServiceAccountExpired, got: %v", err)
	}

	// 2. Least-privilege scope validation
	futureTime := time.Now().Add(24 * time.Hour)
	_, validKey, err := testApp.CreateServiceAccount(
		"Read Only Worker",
		"",
		"Worker with strictly read scope",
		[]string{"records.read"},
		[]string{"*"},
		&futureTime,
	)
	if err != nil {
		t.Fatalf("Failed to create read-only account: %v", err)
	}

	sp, err := testApp.AuthenticateServiceAccount(validKey, "")
	if err != nil {
		t.Fatalf("Failed to authenticate: %v", err)
	}

	if !sp.HasPermission("records.read") {
		t.Errorf("Expected records.read permission")
	}
	// Privilege escalation prevention: must NOT have write or delete permissions
	if sp.HasPermission("records.delete") {
		t.Errorf("Read-only service account must NOT have records.delete permission")
	}
	if sp.HasPermission("members.manage") {
		t.Errorf("Read-only service account must NOT have members.manage permission")
	}
}

func TestServiceAccounts_TenantIsolation(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tenant1, err := testApp.CreateTenant("Svc Tenant 1", "svc-t1")
	if err != nil {
		t.Fatalf("Failed to create tenant 1: %v", err)
	}
	tenant2, err := testApp.CreateTenant("Svc Tenant 2", "svc-t2")
	if err != nil {
		t.Fatalf("Failed to create tenant 2: %v", err)
	}

	// 1. Service account strictly bound to tenant1
	_, rawKeyBound, err := testApp.CreateServiceAccount(
		"Tenant 1 Ingestion Bot",
		tenant1.Id,
		"Ingestion bot for tenant 1 only",
		[]string{"records.create"},
		[]string{tenant1.Id},
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create tenant-bound service account: %v", err)
	}

	// Authenticate for tenant 1 -> succeeds
	sp1, err := testApp.AuthenticateServiceAccount(rawKeyBound, tenant1.Id)
	if err != nil {
		t.Fatalf("Expected success authenticating for authorized tenant: %v", err)
	}
	if sp1.TenantId() != tenant1.Id {
		t.Errorf("Expected active tenant %s, got %s", tenant1.Id, sp1.TenantId())
	}

	// Authenticate for tenant 2 -> strictly rejected
	_, err = testApp.AuthenticateServiceAccount(rawKeyBound, tenant2.Id)
	if !errors.Is(err, core.ErrServiceAccountTenantUnauthorized) {
		t.Errorf("Expected ErrServiceAccountTenantUnauthorized when requesting unauthorized tenant, got: %v", err)
	}
}
