package e2e_test

import (
	"context"
	"errors"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestRBAC_SystemRolesAndDefaults(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	if err := testApp.EnsureRBACSystemTables(); err != nil {
		t.Fatalf("Failed to ensure RBAC system tables: %v", err)
	}

	// 1. Verify all 4 default system roles exist
	roles, err := testApp.ListRoles("")
	if err != nil {
		t.Fatalf("Failed to list system roles: %v", err)
	}
	if len(roles) < 4 {
		t.Fatalf("Expected at least 4 system roles, got %d", len(roles))
	}

	expectedSystemRoles := map[string][]string{
		core.RoleOwner:  {"*"},
		core.RoleAdmin:  {"records.*", "files.*", "members.manage"},
		core.RoleMember: {"records.read", "records.create", "records.update", "files.upload"},
		core.RoleGuest:  {"records.read"},
	}

	for roleName, expectedPerms := range expectedSystemRoles {
		r, err := testApp.GetRole("", roleName)
		if err != nil {
			t.Fatalf("Expected system role %q to exist, got err: %v", roleName, err)
		}
		if !r.IsSystem {
			t.Errorf("Expected role %q to have IsSystem=true", roleName)
		}
		for _, p := range expectedPerms {
			matched := false
			for _, actual := range r.Permissions {
				if actual == p {
					matched = true
					break
				}
			}
			if !matched {
				t.Errorf("Role %q missing expected permission %q (has %v)", roleName, p, r.Permissions)
			}
		}
	}

	// 2. Verify system role immutability
	err = testApp.DeleteRole("", core.RoleAdmin)
	if !errors.Is(err, core.ErrSystemRoleImmutable) {
		t.Errorf("Expected ErrSystemRoleImmutable when deleting system role, got: %v", err)
	}

	err = testApp.UpdateRole("", core.RoleAdmin, "New Desc", []string{"*"})
	if !errors.Is(err, core.ErrSystemRoleImmutable) {
		t.Errorf("Expected ErrSystemRoleImmutable when updating system role, got: %v", err)
	}
}

func TestRBAC_CustomTenantRoles(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tenant, err := testApp.CreateTenant("Beta Org", "beta-org")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	// 1. Create a custom role in the tenant
	customRole, err := testApp.CreateRole(tenant.Id, "content_editor", "Content editor role", []string{"records.read", "records.update"})
	if err != nil {
		t.Fatalf("Failed to create custom role: %v", err)
	}
	if customRole.Name != "content_editor" {
		t.Errorf("Expected role name 'content_editor', got %s", customRole.Name)
	}
	if customRole.IsSystem {
		t.Errorf("Expected IsSystem=false for tenant custom role")
	}

	// 2. Prevent duplicate role in same tenant
	_, err = testApp.CreateRole(tenant.Id, "content_editor", "Duplicate", []string{"records.read"})
	if !errors.Is(err, core.ErrRoleAlreadyExists) {
		t.Errorf("Expected ErrRoleAlreadyExists on duplicate role, got: %v", err)
	}

	// 3. Update custom role
	err = testApp.UpdateRole(tenant.Id, "content_editor", "Updated editor", []string{"records.read", "records.update", "files.upload"})
	if err != nil {
		t.Fatalf("Failed to update custom role: %v", err)
	}

	fetched, err := testApp.GetRole(tenant.Id, "content_editor")
	if err != nil {
		t.Fatalf("Failed to get updated role: %v", err)
	}
	if len(fetched.Permissions) != 3 {
		t.Errorf("Expected 3 permissions after update, got %d", len(fetched.Permissions))
	}

	// 4. List tenant roles (should include system defaults and custom role)
	tenantRoles, err := testApp.ListRoles(tenant.Id)
	if err != nil {
		t.Fatalf("Failed to list tenant roles: %v", err)
	}
	if len(tenantRoles) < 5 { // 4 system defaults + 1 custom
		t.Errorf("Expected at least 5 roles in tenant listing, got %d", len(tenantRoles))
	}

	// 5. Delete custom role
	if err := testApp.DeleteRole(tenant.Id, "content_editor"); err != nil {
		t.Fatalf("Failed to delete custom role: %v", err)
	}

	_, err = testApp.GetRole(tenant.Id, "content_editor")
	if !errors.Is(err, core.ErrRoleNotFound) {
		t.Errorf("Expected ErrRoleNotFound after deleting role, got: %v", err)
	}
}

func TestRBAC_EvaluateAccessBoundaries(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	ctx := context.Background()

	// 1. Create 2 tenants
	tenantA, err := testApp.CreateTenant("Tenant A", "tenant-a")
	if err != nil {
		t.Fatalf("Failed to create tenant A: %v", err)
	}
	tenantB, err := testApp.CreateTenant("Tenant B", "tenant-b")
	if err != nil {
		t.Fatalf("Failed to create tenant B: %v", err)
	}

	// 2. Create users in collection
	usersColl, err := testApp.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users: %v", err)
	}

	ownerUser := core.NewRecord(usersColl)
	ownerUser.SetEmail("owner@tenant-a.com")
	ownerUser.SetPassword("1234567890")
	if err := testApp.Save(ownerUser); err != nil {
		t.Fatalf("Failed to save owner: %v", err)
	}

	memberUser := core.NewRecord(usersColl)
	memberUser.SetEmail("member@tenant-a.com")
	memberUser.SetPassword("1234567890")
	if err := testApp.Save(memberUser); err != nil {
		t.Fatalf("Failed to save member: %v", err)
	}

	guestUser := core.NewRecord(usersColl)
	guestUser.SetEmail("guest@tenant-a.com")
	guestUser.SetPassword("1234567890")
	if err := testApp.Save(guestUser); err != nil {
		t.Fatalf("Failed to save guest: %v", err)
	}

	// Add memberships in Tenant A
	mOwner, _ := testApp.AddTenantMember(tenantA.Id, ownerUser.Id, core.RoleOwner)
	mMember, _ := testApp.AddTenantMember(tenantA.Id, memberUser.Id, core.RoleMember)
	mGuest, _ := testApp.AddTenantMember(tenantA.Id, guestUser.Id, core.RoleGuest)

	pOwner := core.NewUserPrincipal(ownerUser.Id, "users", tenantA.Id, []*core.TenantMembership{mOwner})
	pMember := core.NewUserPrincipal(memberUser.Id, "users", tenantA.Id, []*core.TenantMembership{mMember})
	pGuest := core.NewUserPrincipal(guestUser.Id, "users", tenantA.Id, []*core.TenantMembership{mGuest})
	pSuper := core.NewSuperuserPrincipal("root_admin", "")

	resourceA := &core.ResourceContext{
		Collection: "posts",
		Id:         "post_001",
		TenantId:   tenantA.Id,
		CreatedBy:  ownerUser.Id,
	}

	resourceB := &core.ResourceContext{
		Collection: "posts",
		Id:         "post_002",
		TenantId:   tenantB.Id,
		CreatedBy:  "someone_else",
	}

	// A. Superuser has access to everything across tenants
	allowed, err := testApp.EvaluateAccess(ctx, pSuper, resourceA, core.PermissionSettingsManage)
	if err != nil || !allowed {
		t.Errorf("Superuser should be granted settings.manage on resourceA, got allowed=%v, err=%v", allowed, err)
	}
	allowed, err = testApp.EvaluateAccess(ctx, pSuper, resourceB, core.PermissionRecordsDelete)
	if err != nil || !allowed {
		t.Errorf("Superuser should be granted records.delete on resourceB, got allowed=%v, err=%v", allowed, err)
	}

	// B. Cross-tenant access denial: pMember belonging to tenantA accessing resourceB (tenantB)
	allowed, err = testApp.EvaluateAccess(ctx, pMember, resourceB, core.PermissionRecordsRead)
	if !errors.Is(err, core.ErrTenantAccessDenied) {
		t.Errorf("Expected ErrTenantAccessDenied for cross-tenant access, got allowed=%v, err=%v", allowed, err)
	}

	// C. Role Owner: full access (* wildcard) in tenantA
	allowed, err = testApp.EvaluateAccess(ctx, pOwner, resourceA, core.PermissionSettingsManage)
	if err != nil || !allowed {
		t.Errorf("Owner should have settings.manage, got allowed=%v, err=%v", allowed, err)
	}
	allowed, err = testApp.EvaluateAccess(ctx, pOwner, resourceA, core.PermissionRecordsDelete)
	if err != nil || !allowed {
		t.Errorf("Owner should have records.delete, got allowed=%v, err=%v", allowed, err)
	}

	// D. Role Member: has records.read and records.create, but NOT settings.manage or global records.delete
	allowed, err = testApp.EvaluateAccess(ctx, pMember, resourceA, core.PermissionRecordsRead)
	if err != nil || !allowed {
		t.Errorf("Member should have records.read, got allowed=%v, err=%v", allowed, err)
	}
	allowed, err = testApp.EvaluateAccess(ctx, pMember, resourceA, core.PermissionSettingsManage)
	if err != nil || allowed {
		t.Errorf("Member should NOT have settings.manage, got allowed=%v, err=%v", allowed, err)
	}

	// E. Role Guest: has records.read, but NOT records.create
	allowed, err = testApp.EvaluateAccess(ctx, pGuest, resourceA, core.PermissionRecordsRead)
	if err != nil || !allowed {
		t.Errorf("Guest should have records.read, got allowed=%v, err=%v", allowed, err)
	}
	allowed, err = testApp.EvaluateAccess(ctx, pGuest, resourceA, core.PermissionRecordsCreate)
	if err != nil || allowed {
		t.Errorf("Guest should NOT have records.create, got allowed=%v, err=%v", allowed, err)
	}

	// F. Suspended tenant fails closed
	if err := testApp.UpdateTenantStatus(tenantA.Id, core.TenantStatusSuspended); err != nil {
		t.Fatalf("Failed to suspend tenant: %v", err)
	}
	allowed, err = testApp.EvaluateAccess(ctx, pMember, resourceA, core.PermissionRecordsRead)
	if !errors.Is(err, core.ErrTenantSuspended) {
		t.Errorf("Expected ErrTenantSuspended for access in suspended tenant, got allowed=%v, err=%v", allowed, err)
	}
}

func TestABAC_ContextualEvaluation(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	ctx := context.Background()

	tenant, err := testApp.CreateTenant("Gamma Corp", "gamma-corp")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	usersColl, err := testApp.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users: %v", err)
	}

	user1 := core.NewRecord(usersColl)
	user1.SetEmail("user1@gamma.com")
	user1.SetPassword("1234567890")
	if err := testApp.Save(user1); err != nil {
		t.Fatalf("Failed to save user1: %v", err)
	}

	user2 := core.NewRecord(usersColl)
	user2.SetEmail("user2@gamma.com")
	user2.SetPassword("1234567890")
	if err := testApp.Save(user2); err != nil {
		t.Fatalf("Failed to save user2: %v", err)
	}

	m1, _ := testApp.AddTenantMember(tenant.Id, user1.Id, core.RoleMember)
	m2, _ := testApp.AddTenantMember(tenant.Id, user2.Id, core.RoleMember)

	p1 := core.NewUserPrincipal(user1.Id, "users", tenant.Id, []*core.TenantMembership{m1})
	p2 := core.NewUserPrincipal(user2.Id, "users", tenant.Id, []*core.TenantMembership{m2})

	// Resource created by user1
	user1Resource := &core.ResourceContext{
		Collection: "documents",
		Id:         "doc_001",
		TenantId:   tenant.Id,
		CreatedBy:  user1.Id,
	}

	// 1. Self-ownership ABAC check:
	// A standard member role does not grant global `records.delete`.
	// But user1 created doc_001 -> ABAC grants self-ownership delete!
	allowed, err := testApp.EvaluateAccess(ctx, p1, user1Resource, core.PermissionRecordsDelete)
	if err != nil || !allowed {
		t.Errorf("Self-ownership should grant records.delete to author user1, got allowed=%v, err=%v", allowed, err)
	}

	// 2. Non-owner member check:
	// User2 did NOT create doc_001 -> user2 cannot delete doc_001!
	allowed, err = testApp.EvaluateAccess(ctx, p2, user1Resource, core.PermissionRecordsDelete)
	if err != nil || allowed {
		t.Errorf("User2 should NOT be allowed to delete resource owned by user1, got allowed=%v, err=%v", allowed, err)
	}

	// 3. Attribute-based assignee check
	assignedResource := &core.ResourceContext{
		Collection: "tasks",
		Id:         "task_001",
		TenantId:   tenant.Id,
		CreatedBy:  "admin_creator",
		Attributes: map[string]any{
			"owner_id": user2.Id,
		},
	}
	allowed, err = testApp.EvaluateAccess(ctx, p2, assignedResource, core.PermissionRecordsUpdate)
	if err != nil || !allowed {
		t.Errorf("Assignee user2 should be allowed to update task assigned to them, got allowed=%v, err=%v", allowed, err)
	}

	// 4. Anonymous public read attribute
	pAnon := &core.AnonymousPrincipal{}
	publicResource := &core.ResourceContext{
		Collection: "articles",
		Id:         "art_001",
		TenantId:   tenant.Id,
		CreatedBy:  user1.Id,
		Attributes: map[string]any{
			"is_public": true,
		},
	}
	privateResource := &core.ResourceContext{
		Collection: "articles",
		Id:         "art_002",
		TenantId:   tenant.Id,
		CreatedBy:  user1.Id,
		Attributes: map[string]any{
			"is_public": false,
		},
	}

	allowed, err = testApp.EvaluateAccess(ctx, pAnon, publicResource, core.PermissionRecordsRead)
	if err != nil || !allowed {
		t.Errorf("Anonymous should be allowed to read public resource, got allowed=%v, err=%v", allowed, err)
	}

	allowed, err = testApp.EvaluateAccess(ctx, pAnon, privateResource, core.PermissionRecordsRead)
	if err != nil || allowed {
		t.Errorf("Anonymous should NOT be allowed to read private resource, got allowed=%v, err=%v", allowed, err)
	}
}
