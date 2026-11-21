package e2e_test

import (
	"errors"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestSessionLifecycle_TenantTokenGenerationAndVerification(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	// 1. Create a user record in "users" collection
	usersColl, err := testApp.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}

	user := core.NewRecord(usersColl)
	user.SetEmail("alice@example.com")
	user.SetPassword("1234567890")
	if err := testApp.Save(user); err != nil {
		t.Fatalf("Failed to save user: %v", err)
	}

	// 2. Create tenant and add user as admin
	tenant, err := testApp.CreateTenant("Alpha Tech", "alpha-tech")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	_, err = testApp.AddTenantMember(tenant.Id, user.Id, core.RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to add tenant member: %v", err)
	}

	// 3. Generate tenant-scoped auth token
	tokenStr, err := testApp.GenerateTenantAuthToken(user, tenant.Id, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate tenant auth token: %v", err)
	}
	if tokenStr == "" {
		t.Fatalf("Generated token is empty")
	}

	// 4. Verify token
	principal, verifiedRecord, err := testApp.VerifyTenantAuthToken(tokenStr)
	if err != nil {
		t.Fatalf("Failed to verify tenant auth token: %v", err)
	}
	if verifiedRecord.Id != user.Id {
		t.Fatalf("Verified record ID mismatch: got %s, expected %s", verifiedRecord.Id, user.Id)
	}
	if principal.TenantId() != tenant.Id {
		t.Fatalf("Principal tenant mismatch: got %s, expected %s", principal.TenantId(), tenant.Id)
	}
	if !principal.HasRole(core.RoleAdmin) {
		t.Fatalf("Expected principal to have admin role, got roles: %v", principal.Roles())
	}
}

func TestSessionLifecycle_InstantRevocationOnTokenKeyRotation(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	usersColl, err := testApp.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}

	user := core.NewRecord(usersColl)
	user.SetEmail("bob@example.com")
	user.SetPassword("1234567890")
	if err := testApp.Save(user); err != nil {
		t.Fatalf("Failed to save user: %v", err)
	}

	tenant, err := testApp.CreateTenant("Beta Labs", "beta-labs")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	_, err = testApp.AddTenantMember(tenant.Id, user.Id, core.RoleMember)
	if err != nil {
		t.Fatalf("Failed to add tenant member: %v", err)
	}

	tokenStr, err := testApp.GenerateTenantAuthToken(user, tenant.Id, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Verify token is valid initially
	_, _, err = testApp.VerifyTenantAuthToken(tokenStr)
	if err != nil {
		t.Fatalf("Token must be valid initially: %v", err)
	}

	// Revoke user session ($T=0 instant invalidation)
	if err := testApp.RevokeUserSession(user); err != nil {
		t.Fatalf("Failed to revoke session: %v", err)
	}

	// Token must be rejected immediately
	_, _, err = testApp.VerifyTenantAuthToken(tokenStr)
	if !errors.Is(err, core.ErrSessionRevoked) {
		t.Fatalf("Expected ErrSessionRevoked after tokenKey rotation, got: %v", err)
	}
}

func TestSessionLifecycle_AuthoritativeClaimFreshnessOnRoleChange(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	usersColl, err := testApp.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}

	user := core.NewRecord(usersColl)
	user.SetEmail("charlie@example.com")
	user.SetPassword("1234567890")
	if err := testApp.Save(user); err != nil {
		t.Fatalf("Failed to save user: %v", err)
	}

	tenant, err := testApp.CreateTenant("Gamma Corp", "gamma-corp")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Add Charlie as Admin initially
	_, err = testApp.AddTenantMember(tenant.Id, user.Id, core.RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to add member: %v", err)
	}

	// Issue token while Charlie is Admin (embedded claim = "admin")
	tokenStr, err := testApp.GenerateTenantAuthToken(user, tenant.Id, 1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// Demote Charlie to Member in database
	if err := testApp.UpdateTenantMemberRole(tenant.Id, user.Id, core.RoleMember); err != nil {
		t.Fatalf("Failed to demote member: %v", err)
	}

	// Verify token: server MUST evaluate fresh database role (member), NOT stale token claim (admin)
	principal, _, err := testApp.VerifyTenantAuthToken(tokenStr)
	if err != nil {
		t.Fatalf("Token verification failed: %v", err)
	}

	if principal.HasRole(core.RoleAdmin) {
		t.Fatalf("SECURITY VIOLATION: Stale token claim permitted evaluated admin role after demotion")
	}
	if !principal.HasRole(core.RoleMember) {
		t.Fatalf("Expected principal to have fresh role 'member', got: %v", principal.Roles())
	}
}

func TestSessionLifecycle_SanitizeClientTamperedClaims(t *testing.T) {
	untrustedPayload := map[string]any{
		"title":              "Valid Document",
		"content":            "Hello World",
		"role":               "owner",
		"tenantId":           "tenant_spoofed_123",
		"@request.auth.role": "superuser",
		"isSuperuser":        true,
		"system":             true,
	}

	sanitized := core.SanitizeClientPayload(untrustedPayload)

	if sanitized["title"] != "Valid Document" || sanitized["content"] != "Hello World" {
		t.Fatalf("Legitimate fields were unexpectedly stripped: %+v", sanitized)
	}
	if _, exists := sanitized["role"]; exists {
		t.Fatalf("Forged 'role' was not sanitized")
	}
	if _, exists := sanitized["tenantId"]; exists {
		t.Fatalf("Forged 'tenantId' was not sanitized")
	}
	if _, exists := sanitized["@request.auth.role"]; exists {
		t.Fatalf("Forged '@request.auth.role' was not sanitized")
	}
	if _, exists := sanitized["isSuperuser"]; exists {
		t.Fatalf("Forged 'isSuperuser' was not sanitized")
	}
	if _, exists := sanitized["system"]; exists {
		t.Fatalf("Forged 'system' was not sanitized")
	}
}
