package core

import (
	"errors"
	"fmt"
	"strings"

	"github.com/AbdullahZaher/ruxbase/tools/security"
)

var (
	ErrCrossTenantRelationForbidden = errors.New("cross-tenant relational linking is forbidden")
	ErrTenantMismatch               = errors.New("record tenant does not match active principal tenant")
	ErrTenantFileAccessDenied       = errors.New("access to tenant file denied")
)

const (
	TenantIdField = "tenant_id"
)

// EnableTenantRLS enables Row-Level Security on the specified table,
// installing the tenant_isolation_policy for strict SQL-level tenant isolation.
func (app *BaseApp) EnableTenantRLS(tableName string) error {
	quotedTable := quoteRLSIdent(tableName)

	query := fmt.Sprintf(`
		GRANT SELECT, INSERT, UPDATE, DELETE ON %s TO %s, %s, %s;
		ALTER TABLE %s ENABLE ROW LEVEL SECURITY;
		DROP POLICY IF EXISTS tenant_isolation_policy ON %s;
		CREATE POLICY tenant_isolation_policy ON %s
			FOR ALL
			TO %s, %s, %s
			USING (tenant_id = NULLIF(current_setting('%s', true), ''))
			WITH CHECK (tenant_id = NULLIF(current_setting('%s', true), ''));
	`, quotedTable, RLSAnonRole, RLSAuthenticatedRole, RLSAPIRole,
		quotedTable, quotedTable, quotedTable,
		RLSAnonRole, RLSAuthenticatedRole, RLSAPIRole,
		RLSTenantSetting, RLSTenantSetting)

	_, err := app.NonconcurrentDB().NewQuery(query).Execute()
	if err != nil {
		return fmt.Errorf("failed to enable tenant RLS on table %s: %w", tableName, err)
	}

	return nil
}

// DisableTenantRLS drops the tenant isolation policy and disables RLS on the table.
func (app *BaseApp) DisableTenantRLS(tableName string) error {
	quotedTable := quoteRLSIdent(tableName)

	query := fmt.Sprintf(`
		DROP POLICY IF EXISTS tenant_isolation_policy ON %s;
		ALTER TABLE %s DISABLE ROW LEVEL SECURITY;
	`, quotedTable, quotedTable)

	_, err := app.NonconcurrentDB().NewQuery(query).Execute()
	if err != nil {
		return fmt.Errorf("failed to disable tenant RLS on table %s: %w", tableName, err)
	}

	return nil
}

// EnsureTenantCollection ensures that a collection has a tenant_id field and index,
// and enforces tenant RLS on its underlying PostgreSQL table.
func (app *BaseApp) EnsureTenantCollection(collection *Collection) error {
	if collection == nil {
		return errors.New("collection cannot be nil")
	}

	// 1. Ensure tenant_id field is present in collection schema
	field := collection.Fields.GetByName(TenantIdField)
	if field == nil {
		collection.Fields.Add(&TextField{
			Name:     TenantIdField,
			Required: true,
			Max:      15,
		})
	}

	// 2. Ensure index on tenant_id exists
	idxName := fmt.Sprintf("idx_%s_tenant_id", collection.Name)
	hasIdx := false
	for _, idx := range collection.Indexes {
		if strings.Contains(idx, idxName) || strings.Contains(idx, TenantIdField) {
			hasIdx = true
			break
		}
	}
	if !hasIdx {
		collection.Indexes = append(collection.Indexes, fmt.Sprintf("CREATE INDEX %s ON %s (tenant_id)", idxName, collection.Name))
	}

	// 3. Save collection metadata
	if err := app.Save(collection); err != nil {
		return fmt.Errorf("failed to save tenant collection %s: %w", collection.Name, err)
	}

	// 4. Enable PostgreSQL RLS on the physical table
	if err := app.EnableTenantRLS(collection.Name); err != nil {
		return err
	}

	return nil
}

// ValidateTenantRelation verifies that two related records belong to the same tenant.
func (app *BaseApp) ValidateTenantRelation(sourceTenantId, targetTenantId string) error {
	if sourceTenantId == "" || targetTenantId == "" {
		return nil // Non-tenant entities or global entities are permitted
	}
	if sourceTenantId != targetTenantId {
		return ErrCrossTenantRelationForbidden
	}
	return nil
}

// ValidateRecordTenant ensures that a record being saved adheres to the authorized tenant context.
func (app *BaseApp) ValidateRecordTenant(record *Record, activeTenantId string) error {
	if record == nil {
		return errors.New("record cannot be nil")
	}

	// If collection doesn't have a tenant_id field, skip
	if record.Collection().Fields.GetByName(TenantIdField) == nil {
		return nil
	}

	recordTenant := record.GetString(TenantIdField)

	// If record doesn't have tenant set and active context exists, inherit active tenant
	if recordTenant == "" && activeTenantId != "" {
		record.Set(TenantIdField, activeTenantId)
		return nil
	}

	// If record has tenant set and active context exists, verify they match
	if recordTenant != "" && activeTenantId != "" && recordTenant != activeTenantId {
		return ErrTenantMismatch
	}

	// If both are empty, require tenant context
	if recordTenant == "" && activeTenantId == "" {
		return ErrTenantContextMissing
	}

	return nil
}

// BuildTenantStoragePath creates a deterministic, tenant-isolated storage path for files.
func BuildTenantStoragePath(tenantId, collectionId, recordId, filename string) string {
	tenantId = strings.TrimSpace(tenantId)
	if tenantId == "" {
		tenantId = "_global"
	}
	// Sanitize filename to avoid path traversal
	cleanFile := security.RandomString(10) + "_" + strings.ReplaceAll(filename, "/", "_")
	return fmt.Sprintf("tenants/%s/%s/%s/%s", tenantId, collectionId, recordId, cleanFile)
}

// ValidateTenantFileAccess verifies that a principal is authorized to access a tenant file path.
func (app *BaseApp) ValidateTenantFileAccess(principal Principal, storagePath string) error {
	if principal == nil {
		return ErrTenantFileAccessDenied
	}

	// Superuser bypass
	if principal.Type() == PrincipalTypeSuperuser {
		return nil
	}

	// Path expected format: tenants/<tenant_id>/...
	parts := strings.Split(strings.TrimPrefix(storagePath, "/"), "/")
	if len(parts) >= 2 && parts[0] == "tenants" {
		pathTenantId := parts[1]
		if pathTenantId == "_global" {
			return nil
		}
		if principal.TenantId() != pathTenantId {
			return ErrTenantFileAccessDenied
		}
		return app.ValidateTenantAccess(principal, pathTenantId)
	}

	return nil
}

// FilterTenantRealtimeEvent evaluates whether a realtime event should be dispatched to a principal.
func (app *BaseApp) FilterTenantRealtimeEvent(principal Principal, eventTenantId string) bool {
	if principal == nil {
		return false
	}

	// Superuser receives all events
	if principal.Type() == PrincipalTypeSuperuser {
		return true
	}

	// Global events dispatched to all authenticated callers
	if eventTenantId == "" || eventTenantId == "_global" {
		return true
	}

	// Tenant-specific event must match principal's tenant
	return principal.TenantId() != "" && principal.TenantId() == eventTenantId
}
