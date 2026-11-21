package e2e_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

func TestTenantOwnership_SchemaAndRLSEnforcement(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	// 1. Create two separate tenants
	tenant1, err := testApp.CreateTenant("Tenant One", "tenant-one")
	if err != nil {
		t.Fatalf("Failed to create tenant 1: %v", err)
	}
	tenant2, err := testApp.CreateTenant("Tenant Two", "tenant-two")
	if err != nil {
		t.Fatalf("Failed to create tenant 2: %v", err)
	}

	// 2. Create a collection "projects" and bind it to tenant RLS
	projectsColl := core.NewBaseCollection("projects")
	projectsColl.Fields.Add(&core.TextField{
		Name:     "title",
		Required: true,
	})
	if err := testApp.Save(projectsColl); err != nil {
		t.Fatalf("Failed to save projects collection: %v", err)
	}

	if err := testApp.EnsureTenantCollection(projectsColl); err != nil {
		t.Fatalf("Failed to ensure tenant collection: %v", err)
	}

	// Verify field exists
	if projectsColl.Fields.GetByName(core.TenantIdField) == nil {
		t.Fatalf("Expected %s field in collection schema", core.TenantIdField)
	}

	// 3. Insert records for tenant 1 and tenant 2 as admin/superuser
	rec1 := core.NewRecord(projectsColl)
	rec1.Set("title", "Project Alpha")
	rec1.Set("tenant_id", tenant1.Id)
	if err := testApp.Save(rec1); err != nil {
		t.Fatalf("Failed to save rec1: %v", err)
	}

	rec2 := core.NewRecord(projectsColl)
	rec2.Set("title", "Project Beta")
	rec2.Set("tenant_id", tenant2.Id)
	if err := testApp.Save(rec2); err != nil {
		t.Fatalf("Failed to save rec2: %v", err)
	}

	// 4. Test RLS under restricted role "ruxbase_authenticated" scoped to Tenant 1
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tenant1.Id,
	}, func(txApp core.App) error {
		var titles []string
		err := txApp.DB().Select("title").From("projects").Column(&titles)
		if err != nil {
			return err
		}
		if len(titles) != 1 || titles[0] != "Project Alpha" {
			return errors.New("Tenant 1 query should only return Project Alpha")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tenant 1 scoped query failed: %v", err)
	}

	// 5. Test RLS under restricted role "ruxbase_authenticated" scoped to Tenant 2
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tenant2.Id,
	}, func(txApp core.App) error {
		var titles []string
		err := txApp.DB().Select("title").From("projects").Column(&titles)
		if err != nil {
			return err
		}
		if len(titles) != 1 || titles[0] != "Project Beta" {
			return errors.New("Tenant 2 query should only return Project Beta")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Tenant 2 scoped query failed: %v", err)
	}

	// 6. Test RLS fail-closed: No tenant context provided under ruxbase_authenticated
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: "",
	}, func(txApp core.App) error {
		var titles []string
		err := txApp.DB().Select("title").From("projects").Column(&titles)
		if err != nil {
			return err
		}
		if len(titles) != 0 {
			return errors.New("Unscoped query under restricted role must return 0 rows (fail closed)")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Unscoped fail-closed test failed: %v", err)
	}

	// 7. Test RLS WITH CHECK constraint: Tenant 1 cannot insert row for Tenant 2
	err = testApp.RunInScopedTransaction(core.TransactionScope{
		Role:     core.RLSAuthenticatedRole,
		TenantID: tenant1.Id,
	}, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			INSERT INTO projects (id, title, tenant_id)
			VALUES ('bad_rec_001', 'Illegitimate Project', {:targetTenant})
		`).Bind(dbx.Params{"targetTenant": tenant2.Id}).Execute()
		return err
	})
	if err == nil {
		t.Fatalf("Expected RLS WITH CHECK policy violation when inserting mismatched tenant record, got nil")
	}
	if !strings.Contains(err.Error(), "violates row-level security policy") {
		t.Fatalf("Expected row-level security policy error, got: %v", err)
	}
}

func TestTenantOwnership_RelationalIntegrity(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tenant1, err := testApp.CreateTenant("Org 1", "org-1")
	if err != nil {
		t.Fatalf("Failed to create tenant 1: %v", err)
	}
	tenant2, err := testApp.CreateTenant("Org 2", "org-2")
	if err != nil {
		t.Fatalf("Failed to create tenant 2: %v", err)
	}

	// 1. Relational validation
	err = testApp.ValidateTenantRelation(tenant1.Id, tenant1.Id)
	if err != nil {
		t.Errorf("Same tenant relation should be allowed, got: %v", err)
	}

	err = testApp.ValidateTenantRelation(tenant1.Id, tenant2.Id)
	if !errors.Is(err, core.ErrCrossTenantRelationForbidden) {
		t.Errorf("Expected ErrCrossTenantRelationForbidden, got: %v", err)
	}

	// 2. Record validation & tenant inheritance
	coll := core.NewBaseCollection("items")
	coll.Fields.Add(&core.TextField{Name: "tenant_id", Required: true})
	coll.Fields.Add(&core.TextField{Name: "name", Required: true})
	if err := testApp.Save(coll); err != nil {
		t.Fatalf("Failed to save coll: %v", err)
	}

	rec := core.NewRecord(coll)
	rec.Set("name", "Item Alpha")

	// Inherit active tenant
	if err := testApp.ValidateRecordTenant(rec, tenant1.Id); err != nil {
		t.Fatalf("Failed to inherit active tenant: %v", err)
	}
	if rec.GetString("tenant_id") != tenant1.Id {
		t.Errorf("Expected inherited tenant %s, got %s", tenant1.Id, rec.GetString("tenant_id"))
	}

	// Reject mismatched tenant
	rec.Set("tenant_id", tenant2.Id)
	err = testApp.ValidateRecordTenant(rec, tenant1.Id)
	if !errors.Is(err, core.ErrTenantMismatch) {
		t.Errorf("Expected ErrTenantMismatch, got: %v", err)
	}

	// Fail closed if tenant missing on both record and context
	recEmpty := core.NewRecord(coll)
	err = testApp.ValidateRecordTenant(recEmpty, "")
	if !errors.Is(err, core.ErrTenantContextMissing) {
		t.Errorf("Expected ErrTenantContextMissing, got: %v", err)
	}
}

func TestTenantOwnership_FileAndEventIsolation(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	tenant1, err := testApp.CreateTenant("Tenant 100", "tenant-100")
	if err != nil {
		t.Fatalf("Failed to create tenant: %v", err)
	}

	usersColl, err := testApp.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users: %v", err)
	}

	user := core.NewRecord(usersColl)
	user.SetEmail("u100@test.com")
	user.SetPassword("1234567890")
	if err := testApp.Save(user); err != nil {
		t.Fatalf("Failed to save user: %v", err)
	}

	m, _ := testApp.AddTenantMember(tenant1.Id, user.Id, core.RoleMember)

	pUser1 := core.NewUserPrincipal(user.Id, "users", tenant1.Id, []*core.TenantMembership{m})
	pUser2 := core.NewUserPrincipal("other_user", "users", "tenant_200", nil)
	pSuper := core.NewSuperuserPrincipal("admin_root", "")

	// 1. Storage path validation
	path := core.BuildTenantStoragePath(tenant1.Id, "docs", "rec_01", "invoice.pdf")

	// Tenant 1 user has access
	if err := testApp.ValidateTenantFileAccess(pUser1, path); err != nil {
		t.Errorf("Expected pUser1 to have file access, got: %v", err)
	}

	// Tenant 2 user denied
	err = testApp.ValidateTenantFileAccess(pUser2, path)
	if !errors.Is(err, core.ErrTenantFileAccessDenied) {
		t.Errorf("Expected ErrTenantFileAccessDenied for pUser2, got: %v", err)
	}

	// Superuser has access
	if err := testApp.ValidateTenantFileAccess(pSuper, path); err != nil {
		t.Errorf("Superuser should have file access, got: %v", err)
	}

	// 2. Realtime event filtering
	// Event in tenant1
	if !testApp.FilterTenantRealtimeEvent(pUser1, tenant1.Id) {
		t.Errorf("pUser1 should receive event in tenant1")
	}
	if testApp.FilterTenantRealtimeEvent(pUser2, tenant1.Id) {
		t.Errorf("pUser2 must NOT receive event in tenant1")
	}
	if !testApp.FilterTenantRealtimeEvent(pSuper, tenant1.Id) {
		t.Errorf("Superuser should receive all events")
	}
	if !testApp.FilterTenantRealtimeEvent(pUser1, "") {
		t.Errorf("pUser1 should receive global event")
	}
}
