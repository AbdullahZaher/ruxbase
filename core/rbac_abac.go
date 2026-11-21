package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

// Standard permissions catalog
const (
	PermissionWildcard        = "*"
	PermissionRecordsWildcard = "records.*"
	PermissionRecordsRead     = "records.read"
	PermissionRecordsCreate   = "records.create"
	PermissionRecordsUpdate   = "records.update"
	PermissionRecordsDelete   = "records.delete"
	PermissionFilesWildcard   = "files.*"
	PermissionFilesUpload     = "files.upload"
	PermissionFilesDelete     = "files.delete"
	PermissionMembersManage   = "members.manage"
	PermissionSettingsManage  = "settings.manage"
)

var (
	ErrRoleNotFound        = errors.New("role not found")
	ErrRoleAlreadyExists   = errors.New("role already exists")
	ErrSystemRoleImmutable = errors.New("system role cannot be deleted or renamed")
	ErrInvalidRoleName     = errors.New("invalid role name")
)

// Role represents a defined set of permissions within a tenant or as a system default.
type Role struct {
	Id          string                  `db:"id" json:"id"`
	TenantId    string                  `db:"tenant_id" json:"tenantId"`
	Name        string                  `db:"name" json:"name"`
	Description string                  `db:"description" json:"description"`
	Permissions types.JSONArray[string] `db:"permissions" json:"permissions"`
	IsSystem    bool                    `db:"is_system" json:"isSystem"`
	Created     types.DateTime          `db:"created" json:"created"`
	Updated     types.DateTime          `db:"updated" json:"updated"`
}

// TableName returns the physical table name for Roles.
func (r *Role) TableName() string {
	return "_roles"
}

// ResourceContext represents contextual attributes of an accessed resource for ABAC evaluation.
type ResourceContext struct {
	Collection string
	Id         string
	TenantId   string
	CreatedBy  string
	Attributes map[string]any
}

// PermissionMatches returns true if the permission pattern matches the action.
// Supports exact match ("records.read"), wildcard prefix ("records.*"), or global wildcard ("*").
func PermissionMatches(pattern, action string) bool {
	if pattern == PermissionWildcard || pattern == action {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		if strings.HasPrefix(action, prefix+".") {
			return true
		}
	}
	return false
}

// DefaultSystemRoles returns the canonical set of built-in roles and permissions.
func DefaultSystemRoles() map[string][]string {
	return map[string][]string{
		RoleOwner: {
			PermissionWildcard,
		},
		RoleAdmin: {
			PermissionRecordsWildcard,
			PermissionFilesWildcard,
			PermissionMembersManage,
		},
		RoleMember: {
			PermissionRecordsRead,
			PermissionRecordsCreate,
			PermissionRecordsUpdate,
			PermissionFilesUpload,
		},
		RoleGuest: {
			PermissionRecordsRead,
		},
	}
}

// EnsureRBACSystemTables initializes the _roles physical table and default system roles.
func (app *BaseApp) EnsureRBACSystemTables() error {
	if err := app.EnsureIAMSystemTables(); err != nil {
		return err
	}

	return app.RunInTransaction(func(txApp App) error {
		_, err := txApp.NonconcurrentDB().NewQuery(`
			CREATE TABLE IF NOT EXISTS _roles (
				id VARCHAR(15) PRIMARY KEY,
				tenant_id VARCHAR(15) NOT NULL DEFAULT '',
				name VARCHAR(64) NOT NULL,
				description TEXT NOT NULL DEFAULT '',
				permissions JSONB NOT NULL DEFAULT '[]'::jsonb,
				is_system BOOLEAN NOT NULL DEFAULT false,
				created TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
			CREATE UNIQUE INDEX IF NOT EXISTS uq_roles_tenant_name ON _roles (tenant_id, name);
			CREATE INDEX IF NOT EXISTS idx_roles_tenant ON _roles (tenant_id);
		`).Execute()
		if err != nil {
			return fmt.Errorf("failed to ensure _roles table: %w", err)
		}

		// Seed default system roles if they don't already exist
		defaults := DefaultSystemRoles()
		for roleName, perms := range defaults {
			var existingId string
			err := txApp.NonconcurrentDB().Select("id").
				From("_roles").
				Where(dbx.HashExp{"tenant_id": "", "name": roleName}).
				Row(&existingId)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if existingId == "" {
				role := &Role{
					Id:          security.RandomString(15),
					TenantId:    "",
					Name:        roleName,
					Description: fmt.Sprintf("Built-in system %s role", roleName),
					Permissions: types.JSONArray[string](perms),
					IsSystem:    true,
					Created:     types.NowDateTime(),
					Updated:     types.NowDateTime(),
				}
				if err := txApp.NonconcurrentDB().Model(role).Insert(); err != nil {
					return fmt.Errorf("failed to seed system role %s: %w", roleName, err)
				}
			}
		}

		return nil
	})
}

// CreateRole creates a new custom role within a tenant.
func (app *BaseApp) CreateRole(tenantId, name, description string, permissions []string) (*Role, error) {
	if err := app.EnsureRBACSystemTables(); err != nil {
		return nil, err
	}

	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil, ErrInvalidRoleName
	}

	// Verify tenant exists if tenantId is specified
	if tenantId != "" {
		if _, err := app.GetTenant(tenantId); err != nil {
			return nil, err
		}
	}

	// Check if already exists in this tenant
	existing, err := app.GetRole(tenantId, name)
	if err == nil && existing != nil {
		return nil, ErrRoleAlreadyExists
	}

	role := &Role{
		Id:          security.RandomString(15),
		TenantId:    tenantId,
		Name:        name,
		Description: strings.TrimSpace(description),
		Permissions: types.JSONArray[string](permissions),
		IsSystem:    false,
		Created:     types.NowDateTime(),
		Updated:     types.NowDateTime(),
	}

	err = app.NonconcurrentDB().Model(role).Insert()
	if err != nil {
		return nil, fmt.Errorf("failed to create role: %w", err)
	}

	return role, nil
}

// GetRole retrieves a role by tenant ID and name.
// If not found in the tenant and tenantId is not empty, falls back to the system default role.
func (app *BaseApp) GetRole(tenantId, name string) (*Role, error) {
	if err := app.EnsureRBACSystemTables(); err != nil {
		return nil, err
	}

	name = strings.ToLower(strings.TrimSpace(name))

	// 1. Check tenant-specific role first
	if tenantId != "" {
		role := &Role{}
		err := app.NonconcurrentDB().Select("*").
			From("_roles").
			Where(dbx.HashExp{"tenant_id": tenantId, "name": name}).
			One(role)
		if err == nil {
			return role, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}

	// 2. Fall back to system default role
	role := &Role{}
	err := app.NonconcurrentDB().Select("*").
		From("_roles").
		Where(dbx.HashExp{"tenant_id": "", "name": name}).
		One(role)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}

	return role, nil
}

// UpdateRole updates the description and permissions of a custom role.
func (app *BaseApp) UpdateRole(tenantId, name, description string, permissions []string) error {
	role, err := app.GetRole(tenantId, name)
	if err != nil {
		return err
	}

	if role.IsSystem && tenantId == "" {
		return ErrSystemRoleImmutable
	}

	role.Description = strings.TrimSpace(description)
	role.Permissions = types.JSONArray[string](permissions)
	role.Updated = types.NowDateTime()

	return app.NonconcurrentDB().Model(role).Update()
}

// DeleteRole removes a custom role from a tenant. System roles cannot be deleted.
func (app *BaseApp) DeleteRole(tenantId, name string) error {
	role, err := app.GetRole(tenantId, name)
	if err != nil {
		return err
	}

	if role.IsSystem {
		return ErrSystemRoleImmutable
	}

	return app.NonconcurrentDB().Model(role).Delete()
}

// ListRoles lists all roles available for a tenant (including custom roles and system defaults).
func (app *BaseApp) ListRoles(tenantId string) ([]*Role, error) {
	if err := app.EnsureRBACSystemTables(); err != nil {
		return nil, err
	}

	var roles []*Role
	err := app.NonconcurrentDB().Select("*").
		From("_roles").
		Where(dbx.Or(
			dbx.HashExp{"tenant_id": ""},
			dbx.HashExp{"tenant_id": tenantId},
		)).
		OrderBy("is_system DESC", "name ASC").
		All(&roles)

	if err != nil {
		return nil, err
	}

	return roles, nil
}

// EvaluateAccess evaluates whether a principal is authorized to perform an action on a resource under RBAC/ABAC rules.
func (app *BaseApp) EvaluateAccess(ctx context.Context, principal Principal, resource *ResourceContext, action string) (bool, error) {
	if principal == nil {
		return false, errors.New("principal is required for access evaluation")
	}

	// 1. Superuser bypass: Unrestricted administrative authority
	if principal.Type() == PrincipalTypeSuperuser {
		return true, nil
	}

	// 2. Anonymous caller handling
	if principal.Type() == PrincipalTypeAnonymous {
		// Anonymous allowed only if resource is explicitly public and action is read
		if resource != nil && resource.Attributes != nil && resource.Attributes["is_public"] == true {
			if action == PermissionRecordsRead {
				// If resource belongs to a tenant, ensure that tenant is active
				if resource.TenantId != "" {
					tenant, err := app.GetTenant(resource.TenantId)
					if err != nil || tenant.Status != TenantStatusActive {
						return false, ErrTenantAccessDenied
					}
				}
				return true, nil
			}
		}
		return false, nil
	}

	// 3. Tenant boundary assertion for authenticated callers:
	// If resource specifies a tenant, principal MUST belong to that tenant
	if resource != nil && resource.TenantId != "" {
		if principal.TenantId() != resource.TenantId {
			return false, ErrTenantAccessDenied
		}

		// Verify that tenant is active and membership is valid in the database
		if err := app.ValidateTenantAccess(principal, resource.TenantId); err != nil {
			return false, err
		}
	}

	// 4. Role resolution (coarse RBAC)
	roles := principal.Roles()

	// If principal is a user and has an active tenant, query authoritative membership
	if principal.TenantId() != "" && principal.Type() == PrincipalTypeUser {
		membership, err := app.GetTenantMembership(principal.TenantId(), principal.Id())
		if err == nil && membership != nil && membership.Status == MembershipStatusActive {
			roles = []string{membership.Role}
		}
	}

	// Expand permissions across all assigned roles
	var allPermissions []string
	for _, rName := range roles {
		roleObj, err := app.GetRole(principal.TenantId(), rName)
		if err == nil && roleObj != nil {
			allPermissions = append(allPermissions, roleObj.Permissions...)
		} else {
			// Fallback to in-memory system default role definition
			if defPerms, ok := DefaultSystemRoles()[rName]; ok {
				allPermissions = append(allPermissions, defPerms...)
			}
		}
	}

	// Check if any role permission satisfies the action
	for _, perm := range allPermissions {
		if PermissionMatches(perm, action) {
			return true, nil
		}
	}

	// 5. Contextual ABAC Evaluation
	// When coarse RBAC does not grant global permission, check attribute-based ownership
	if resource != nil {
		// Self-ownership rule: User can read, update, or delete their own created resource
		if resource.CreatedBy != "" && resource.CreatedBy == principal.Id() {
			switch action {
			case PermissionRecordsRead, PermissionRecordsUpdate, PermissionRecordsDelete:
				// If principal has basic active membership, self-ownership grants access
				if len(roles) > 0 {
					return true, nil
				}
			}
		}

		// Assignee / direct owner attribute match
		if resource.Attributes != nil {
			if ownerId, ok := resource.Attributes["owner_id"].(string); ok && ownerId == principal.Id() {
				if action == PermissionRecordsRead || action == PermissionRecordsUpdate {
					return true, nil
				}
			}
		}
	}

	// 6. Deny by default
	return false, nil
}
