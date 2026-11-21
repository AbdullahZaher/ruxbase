package e2e_test

import (
	"errors"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestIAM_TenantLifecycleAndStatus(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	// 1. Create a tenant
	tenant, err := testApp.CreateTenant("Acme Corporation", "acme-corp")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}
	if tenant.Id == "" || tenant.Slug != "acme-corp" || tenant.Status != core.TenantStatusActive {
		t.Fatalf("Unexpected tenant state: %+v", tenant)
	}

	// 2. Retrieve by ID and by slug
	byId, err := testApp.GetTenant(tenant.Id)
	if err != nil {
		t.Fatalf("Failed to get tenant by ID: %v", err)
	}
	if byId.Id != tenant.Id || byId.Name != "Acme Corporation" {
		t.Fatalf("Tenant by ID mismatch: %+v", byId)
	}

	bySlug, err := testApp.GetTenant("acme-corp")
	if err != nil {
		t.Fatalf("Failed to get tenant by slug: %v", err)
	}
	if bySlug.Id != tenant.Id {
		t.Fatalf("Tenant by slug mismatch: %+v", bySlug)
	}

	// 3. User with membership can access active tenant
	mem, err := testApp.AddTenantMember(tenant.Id, "usr_001", core.RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to add tenant member: %v", err)
	}
	userPrincipal := core.NewUserPrincipal("usr_001", "users", tenant.Id, []*core.TenantMembership{mem})
	if err := testApp.ValidateTenantAccess(userPrincipal, tenant.Id); err != nil {
		t.Fatalf("Expected access to active tenant to succeed, got: %v", err)
	}

	// 4. Suspend tenant -> immediate fail closed
	if err := testApp.UpdateTenantStatus(tenant.Id, core.TenantStatusSuspended); err != nil {
		t.Fatalf("Failed to suspend tenant: %v", err)
	}

	err = testApp.ValidateTenantAccess(userPrincipal, tenant.Id)
	if !errors.Is(err, core.ErrTenantSuspended) {
		t.Fatalf("Expected ErrTenantSuspended for suspended tenant, got: %v", err)
	}

	// 5. Archive tenant -> immediate fail closed
	if err := testApp.UpdateTenantStatus(tenant.Id, core.TenantStatusArchived); err != nil {
		t.Fatalf("Failed to archive tenant: %v", err)
	}

	err = testApp.ValidateTenantAccess(userPrincipal, tenant.Id)
	if !errors.Is(err, core.ErrTenantArchived) {
		t.Fatalf("Expected ErrTenantArchived for archived tenant, got: %v", err)
	}
}

func TestIAM_UserPrincipalAndMemberships(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	// 1. Create two separate tenants
	tenantA, err := testApp.CreateTenant("Alpha Inc", "alpha")
	if err != nil {
		t.Fatalf("Failed to create tenant A: %v", err)
	}
	tenantB, err := testApp.CreateTenant("Beta LLC", "beta")
	if err != nil {
		t.Fatalf("Failed to create tenant B: %v", err)
	}

	userId := "usr_alice"

	// 2. Add Alice as admin to tenant A
	memA, err := testApp.AddTenantMember(tenantA.Id, userId, core.RoleAdmin)
	if err != nil {
		t.Fatalf("Failed to add member to tenant A: %v", err)
	}
	if memA.Role != core.RoleAdmin || memA.Status != core.MembershipStatusActive {
		t.Fatalf("Unexpected membership state: %+v", memA)
	}

	// 3. UserPrincipal in Tenant A context
	principal := core.NewUserPrincipal(userId, "users", tenantA.Id, []*core.TenantMembership{memA})
	if !principal.InTenant(tenantA.Id) {
		t.Fatalf("Expected Alice to be in Tenant A")
	}
	if !principal.HasRole(core.RoleAdmin) {
		t.Fatalf("Expected Alice to have admin role in Tenant A")
	}
	if err := testApp.ValidateTenantAccess(principal, tenantA.Id); err != nil {
		t.Fatalf("Expected Alice to have valid access to Tenant A, got: %v", err)
	}

	// 4. Alice attempts cross-tenant access to Tenant B -> must fail closed
	if principal.InTenant(tenantB.Id) {
		t.Fatalf("Alice should not be in Tenant B")
	}
	err = testApp.ValidateTenantAccess(principal, tenantB.Id)
	if !errors.Is(err, core.ErrTenantAccessDenied) {
		t.Fatalf("Expected ErrTenantAccessDenied for cross-tenant access, got: %v", err)
	}

	// 5. Update Alice's role to Owner in Tenant A
	if err := testApp.UpdateTenantMemberRole(tenantA.Id, userId, core.RoleOwner); err != nil {
		t.Fatalf("Failed to update role to owner: %v", err)
	}
	updatedMem, err := testApp.GetTenantMembership(tenantA.Id, userId)
	if err != nil {
		t.Fatalf("Failed to get updated membership: %v", err)
	}
	if updatedMem.Role != core.RoleOwner {
		t.Fatalf("Expected role to be owner, got: %s", updatedMem.Role)
	}

	// 6. List user tenants
	tenants, err := testApp.ListUserTenants(userId)
	if err != nil {
		t.Fatalf("Failed to list user tenants: %v", err)
	}
	if len(tenants) != 1 || tenants[0].Id != tenantA.Id {
		t.Fatalf("Expected 1 tenant (Alpha), got: %+v", tenants)
	}

	// 7. Remove Alice from Tenant A -> access denied immediately
	if err := testApp.RemoveTenantMember(tenantA.Id, userId); err != nil {
		t.Fatalf("Failed to remove member: %v", err)
	}
	err = testApp.ValidateTenantAccess(principal, tenantA.Id)
	if !errors.Is(err, core.ErrTenantAccessDenied) {
		t.Fatalf("Expected ErrTenantAccessDenied after membership removal, got: %v", err)
	}
}

func TestIAM_ServicePrincipalAndScopeBoundaries(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tenantX, err := testApp.CreateTenant("Xavier Systems", "xavier")
	if err != nil {
		t.Fatalf("Failed to create tenant X: %v", err)
	}
	tenantY, err := testApp.CreateTenant("Yosemite Labs", "yosemite")
	if err != nil {
		t.Fatalf("Failed to create tenant Y: %v", err)
	}

	// Service principal permitted only in Tenant X with specific scopes
	srvPrincipal := core.NewServicePrincipal(
		"srv_sync_agent",
		tenantX.Id,
		[]string{"records:read", "records:write"},
		[]string{tenantX.Id}, // allowed tenants
		[]string{"service_sync"},
	)

	if srvPrincipal.Type() != core.PrincipalTypeService {
		t.Fatalf("Expected PrincipalTypeService, got: %s", srvPrincipal.Type())
	}
	if !srvPrincipal.HasPermission("records:read") {
		t.Fatalf("Expected service to have records:read permission")
	}
	if srvPrincipal.HasPermission("admin:all") {
		t.Fatalf("Service should not have admin:all permission")
	}

	// Valid access to Tenant X
	if err := testApp.ValidateTenantAccess(srvPrincipal, tenantX.Id); err != nil {
		t.Fatalf("Expected valid access to Tenant X, got: %v", err)
	}

	// Unauthorized attempt to access Tenant Y -> fail closed
	err = testApp.ValidateTenantAccess(srvPrincipal, tenantY.Id)
	if !errors.Is(err, core.ErrTenantAccessDenied) {
		t.Fatalf("Expected ErrTenantAccessDenied for unauthorized tenant Y, got: %v", err)
	}

	// Attempt access with empty tenant -> fail closed
	err = testApp.ValidateTenantAccess(srvPrincipal, "")
	if !errors.Is(err, core.ErrTenantContextMissing) {
		t.Fatalf("Expected ErrTenantContextMissing for empty tenant, got: %v", err)
	}
}

func TestIAM_SuperuserAndAnonymousBoundaries(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tenant, err := testApp.CreateTenant("Global Holdings", "global")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// Superuser has global access to any tenant
	superuser := core.NewSuperuserPrincipal("admin_root", tenant.Id)
	if superuser.Type() != core.PrincipalTypeSuperuser {
		t.Fatalf("Expected PrincipalTypeSuperuser, got: %s", superuser.Type())
	}
	if !superuser.HasRole("superuser") || !superuser.HasPermission("any_perm") {
		t.Fatalf("Superuser must satisfy any role and permission check")
	}
	if err := testApp.ValidateTenantAccess(superuser, tenant.Id); err != nil {
		t.Fatalf("Superuser must have access to any tenant, got: %v", err)
	}

	// Anonymous caller is strictly denied access
	anon := &core.AnonymousPrincipal{}
	if anon.Type() != core.PrincipalTypeAnonymous {
		t.Fatalf("Expected PrincipalTypeAnonymous, got: %s", anon.Type())
	}
	if anon.HasRole("member") || anon.HasPermission("read") {
		t.Fatalf("Anonymous caller must have zero roles and permissions")
	}
	err = testApp.ValidateTenantAccess(anon, tenant.Id)
	if !errors.Is(err, core.ErrTenantAccessDenied) {
		t.Fatalf("Expected ErrTenantAccessDenied for anonymous caller, got: %v", err)
	}
}
