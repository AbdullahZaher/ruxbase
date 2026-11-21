package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

// PrincipalType defines the category of an authenticated identity.
type PrincipalType string

const (
	PrincipalTypeSuperuser PrincipalType = "superuser"
	PrincipalTypeUser      PrincipalType = "user"
	PrincipalTypeService   PrincipalType = "service"
	PrincipalTypeAnonymous PrincipalType = "anonymous"
)

// TenantStatus defines the lifecycle status of a tenant.
type TenantStatus string

const (
	TenantStatusActive    TenantStatus = "active"
	TenantStatusSuspended TenantStatus = "suspended"
	TenantStatusArchived  TenantStatus = "archived"
)

// MembershipStatus defines the status of a user's membership in a tenant.
type MembershipStatus string

const (
	MembershipStatusActive  MembershipStatus = "active"
	MembershipStatusInvited MembershipStatus = "invited"
	MembershipStatusRevoked MembershipStatus = "revoked"
)

// Standard roles within a tenant
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
	RoleGuest  = "guest"
)

var (
	ErrTenantNotFound       = errors.New("tenant not found")
	ErrTenantSuspended      = errors.New("tenant is suspended")
	ErrTenantArchived       = errors.New("tenant is archived")
	ErrTenantAccessDenied   = errors.New("access to tenant denied")
	ErrMembershipNotFound   = errors.New("tenant membership not found")
	ErrMembershipRevoked    = errors.New("tenant membership has been revoked")
	ErrTenantContextMissing = errors.New("tenant context required")
)

// Tenant represents an organizational boundary.
type Tenant struct {
	Id      string         `db:"id" json:"id"`
	Name    string         `db:"name" json:"name"`
	Slug    string         `db:"slug" json:"slug"`
	Status  TenantStatus   `db:"status" json:"status"`
	Created types.DateTime `db:"created" json:"created"`
	Updated types.DateTime `db:"updated" json:"updated"`
}

// TableName returns the physical table name for Tenants.
func (t *Tenant) TableName() string {
	return "_tenants"
}

// TenantMembership represents a user's membership and assigned role within a tenant.
type TenantMembership struct {
	Id       string           `db:"id" json:"id"`
	TenantId string           `db:"tenant_id" json:"tenantId"`
	UserId   string           `db:"user_id" json:"userId"`
	Role     string           `db:"role" json:"role"`
	Status   MembershipStatus `db:"status" json:"status"`
	Created  types.DateTime   `db:"created" json:"created"`
	Updated  types.DateTime   `db:"updated" json:"updated"`
}

// TableName returns the physical table name for TenantMemberships.
func (m *TenantMembership) TableName() string {
	return "_tenant_memberships"
}

// Principal defines the unified identity interface across human users,
// service accounts, superusers, and anonymous visitors.
type Principal interface {
	Id() string
	Type() PrincipalType
	AuthCollection() string
	TenantId() string
	WithTenant(tenantId string) Principal
	Roles() []string
	Permissions() []string
	HasRole(role string) bool
	HasPermission(perm string) bool
	InTenant(tenantId string) bool
}

// UserPrincipal represents an authenticated human user from an auth collection.
type UserPrincipal struct {
	id             string
	authCollection string
	tenantId       string
	roles          []string
	permissions    []string
	memberships    map[string]*TenantMembership // tenantId -> membership
}

// NewUserPrincipal creates a new UserPrincipal.
func NewUserPrincipal(userId, authCollection, activeTenantId string, memberships []*TenantMembership) *UserPrincipal {
	p := &UserPrincipal{
		id:             userId,
		authCollection: authCollection,
		tenantId:       activeTenantId,
		memberships:    make(map[string]*TenantMembership, len(memberships)),
	}

	for _, m := range memberships {
		p.memberships[m.TenantId] = m
	}

	// Calculate roles for active tenant
	if activeTenantId != "" {
		if m, ok := p.memberships[activeTenantId]; ok && m.Status == MembershipStatusActive {
			p.roles = []string{m.Role}
		}
	}

	return p
}

func (u *UserPrincipal) Id() string             { return u.id }
func (u *UserPrincipal) Type() PrincipalType    { return PrincipalTypeUser }
func (u *UserPrincipal) AuthCollection() string { return u.authCollection }
func (u *UserPrincipal) TenantId() string       { return u.tenantId }

func (u *UserPrincipal) WithTenant(tenantId string) Principal {
	cp := *u
	cp.tenantId = tenantId
	cp.roles = nil
	if m, ok := cp.memberships[tenantId]; ok && m.Status == MembershipStatusActive {
		cp.roles = []string{m.Role}
	}
	return &cp
}

func (u *UserPrincipal) Roles() []string {
	return u.roles
}

func (u *UserPrincipal) Permissions() []string {
	return u.permissions
}

func (u *UserPrincipal) HasRole(role string) bool {
	return slices.Contains(u.roles, role)
}

func (u *UserPrincipal) HasPermission(perm string) bool {
	return slices.Contains(u.permissions, perm)
}

func (u *UserPrincipal) InTenant(tenantId string) bool {
	if tenantId == "" {
		return false
	}
	m, ok := u.memberships[tenantId]
	return ok && m.Status == MembershipStatusActive
}

// ServicePrincipal represents a machine or automated backend identity.
type ServicePrincipal struct {
	id             string
	tenantId       string
	scopes         []string
	allowedTenants []string
	roles          []string
}

// NewServicePrincipal creates a new ServicePrincipal.
func NewServicePrincipal(serviceId, activeTenantId string, scopes, allowedTenants, roles []string) *ServicePrincipal {
	return &ServicePrincipal{
		id:             serviceId,
		tenantId:       activeTenantId,
		scopes:         scopes,
		allowedTenants: allowedTenants,
		roles:          roles,
	}
}

func (s *ServicePrincipal) Id() string             { return s.id }
func (s *ServicePrincipal) Type() PrincipalType    { return PrincipalTypeService }
func (s *ServicePrincipal) AuthCollection() string { return "_services" }
func (s *ServicePrincipal) TenantId() string       { return s.tenantId }

func (s *ServicePrincipal) WithTenant(tenantId string) Principal {
	cp := *s
	cp.tenantId = tenantId
	return &cp
}

func (s *ServicePrincipal) Roles() []string {
	return s.roles
}

func (s *ServicePrincipal) Permissions() []string {
	return s.scopes
}

func (s *ServicePrincipal) HasRole(role string) bool {
	return slices.Contains(s.roles, role)
}

func (s *ServicePrincipal) HasPermission(perm string) bool {
	return slices.Contains(s.scopes, perm)
}

func (s *ServicePrincipal) InTenant(tenantId string) bool {
	if len(s.allowedTenants) == 0 {
		return true // unrestricted machine access if wildcard
	}
	return slices.Contains(s.allowedTenants, tenantId)
}

// SuperuserPrincipal represents an administrative operator with unbounded authority.
type SuperuserPrincipal struct {
	id       string
	tenantId string
}

// NewSuperuserPrincipal creates a new SuperuserPrincipal.
func NewSuperuserPrincipal(id, activeTenantId string) *SuperuserPrincipal {
	return &SuperuserPrincipal{id: id, tenantId: activeTenantId}
}

func (s *SuperuserPrincipal) Id() string             { return s.id }
func (s *SuperuserPrincipal) Type() PrincipalType    { return PrincipalTypeSuperuser }
func (s *SuperuserPrincipal) AuthCollection() string { return "_superusers" }
func (s *SuperuserPrincipal) TenantId() string       { return s.tenantId }
func (s *SuperuserPrincipal) WithTenant(tenantId string) Principal {
	return &SuperuserPrincipal{id: s.id, tenantId: tenantId}
}
func (s *SuperuserPrincipal) Roles() []string                { return []string{"superuser", "admin"} }
func (s *SuperuserPrincipal) Permissions() []string          { return []string{"*"} }
func (s *SuperuserPrincipal) HasRole(role string) bool       { return true }
func (s *SuperuserPrincipal) HasPermission(perm string) bool { return true }
func (s *SuperuserPrincipal) InTenant(tenantId string) bool  { return true }

// AnonymousPrincipal represents an unauthenticated caller.
type AnonymousPrincipal struct{}

func (a *AnonymousPrincipal) Id() string                           { return "" }
func (a *AnonymousPrincipal) Type() PrincipalType                  { return PrincipalTypeAnonymous }
func (a *AnonymousPrincipal) AuthCollection() string               { return "" }
func (a *AnonymousPrincipal) TenantId() string                     { return "" }
func (a *AnonymousPrincipal) WithTenant(tenantId string) Principal { return a }
func (a *AnonymousPrincipal) Roles() []string                      { return nil }
func (a *AnonymousPrincipal) Permissions() []string                { return nil }
func (a *AnonymousPrincipal) HasRole(role string) bool             { return false }
func (a *AnonymousPrincipal) HasPermission(perm string) bool       { return false }
func (a *AnonymousPrincipal) InTenant(tenantId string) bool        { return false }

// Context keys for Principal storage
type contextKey string

const principalContextKey contextKey = "ruxbase_principal"

// RequestEventWithPrincipal injects a Principal into the request context.
func RequestEventWithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey, p)
}

// PrincipalFromContext extracts the Principal from context if present.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	if ctx == nil {
		return nil, false
	}
	p, ok := ctx.Value(principalContextKey).(Principal)
	return p, ok
}

// EnsureIAMSystemTables initializes the _tenants and _tenant_memberships physical tables.
func (app *BaseApp) EnsureIAMSystemTables() error {
	return app.RunInTransaction(func(txApp App) error {
		// 1. Create _tenants table if not exists
		_, err := txApp.NonconcurrentDB().NewQuery(`
			CREATE TABLE IF NOT EXISTS _tenants (
				id VARCHAR(15) PRIMARY KEY,
				name VARCHAR(255) NOT NULL,
				slug VARCHAR(255) NOT NULL UNIQUE,
				status VARCHAR(32) NOT NULL DEFAULT 'active',
				created TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx__tenants_status ON _tenants (status);
			CREATE INDEX IF NOT EXISTS idx__tenants_slug ON _tenants (slug);
		`).Execute()
		if err != nil {
			return fmt.Errorf("failed to ensure _tenants table: %w", err)
		}

		// 2. Create _tenant_memberships table if not exists
		_, err = txApp.NonconcurrentDB().NewQuery(`
			CREATE TABLE IF NOT EXISTS _tenant_memberships (
				id VARCHAR(15) PRIMARY KEY,
				tenant_id VARCHAR(15) NOT NULL REFERENCES _tenants(id) ON DELETE CASCADE,
				user_id VARCHAR(15) NOT NULL,
				role VARCHAR(64) NOT NULL DEFAULT 'member',
				status VARCHAR(32) NOT NULL DEFAULT 'active',
				created TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				CONSTRAINT uq_tenant_user UNIQUE (tenant_id, user_id)
			);
			CREATE INDEX IF NOT EXISTS idx__tenant_memberships_user ON _tenant_memberships (user_id);
			CREATE INDEX IF NOT EXISTS idx__tenant_memberships_tenant ON _tenant_memberships (tenant_id);
			CREATE INDEX IF NOT EXISTS idx__tenant_memberships_status ON _tenant_memberships (status);
		`).Execute()
		if err != nil {
			return fmt.Errorf("failed to ensure _tenant_memberships table: %w", err)
		}

		return nil
	})
}

// CreateTenant creates a new organizational tenant.
func (app *BaseApp) CreateTenant(name, slug string) (*Tenant, error) {
	if err := app.EnsureIAMSystemTables(); err != nil {
		return nil, err
	}

	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil, errors.New("tenant slug cannot be empty")
	}

	tenant := &Tenant{
		Id:      security.RandomString(15),
		Name:    strings.TrimSpace(name),
		Slug:    slug,
		Status:  TenantStatusActive,
		Created: types.NowDateTime(),
		Updated: types.NowDateTime(),
	}

	err := app.NonconcurrentDB().Model(tenant).Insert()
	if err != nil {
		return nil, fmt.Errorf("failed to insert tenant: %w", err)
	}

	return tenant, nil
}

// GetTenant retrieves a tenant by ID or slug.
func (app *BaseApp) GetTenant(idOrSlug string) (*Tenant, error) {
	if err := app.EnsureIAMSystemTables(); err != nil {
		return nil, err
	}

	tenant := &Tenant{}
	err := app.NonconcurrentDB().Select("*").
		From("_tenants").
		Where(dbx.Or(
			dbx.HashExp{"id": idOrSlug},
			dbx.HashExp{"slug": strings.ToLower(idOrSlug)},
		)).
		One(tenant)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTenantNotFound
		}
		return nil, err
	}

	return tenant, nil
}

// UpdateTenantStatus updates a tenant's lifecycle status.
func (app *BaseApp) UpdateTenantStatus(id string, status TenantStatus) error {
	tenant, err := app.GetTenant(id)
	if err != nil {
		return err
	}

	tenant.Status = status
	tenant.Updated = types.NowDateTime()

	return app.NonconcurrentDB().Model(tenant).Update()
}

// AddTenantMember adds a user to a tenant with a given role.
func (app *BaseApp) AddTenantMember(tenantId, userId, role string) (*TenantMembership, error) {
	if err := app.EnsureIAMSystemTables(); err != nil {
		return nil, err
	}

	// Verify tenant exists and is active
	tenant, err := app.GetTenant(tenantId)
	if err != nil {
		return nil, err
	}
	if tenant.Status != TenantStatusActive {
		return nil, fmt.Errorf("cannot add member to inactive tenant (status=%s)", tenant.Status)
	}

	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		role = RoleMember
	}

	membership := &TenantMembership{
		Id:       security.RandomString(15),
		TenantId: tenant.Id,
		UserId:   userId,
		Role:     role,
		Status:   MembershipStatusActive,
		Created:  types.NowDateTime(),
		Updated:  types.NowDateTime(),
	}

	err = app.NonconcurrentDB().Model(membership).Insert()
	if err != nil {
		return nil, fmt.Errorf("failed to insert tenant membership: %w", err)
	}

	return membership, nil
}

// UpdateTenantMemberRole updates a member's role in a tenant.
func (app *BaseApp) UpdateTenantMemberRole(tenantId, userId, newRole string) error {
	membership, err := app.GetTenantMembership(tenantId, userId)
	if err != nil {
		return err
	}

	membership.Role = strings.ToLower(strings.TrimSpace(newRole))
	membership.Updated = types.NowDateTime()

	return app.NonconcurrentDB().Model(membership).Update()
}

// RemoveTenantMember removes a user from a tenant.
func (app *BaseApp) RemoveTenantMember(tenantId, userId string) error {
	membership, err := app.GetTenantMembership(tenantId, userId)
	if err != nil {
		return err
	}

	return app.NonconcurrentDB().Model(membership).Delete()
}

// GetTenantMembership retrieves a specific user's membership within a tenant.
func (app *BaseApp) GetTenantMembership(tenantId, userId string) (*TenantMembership, error) {
	if err := app.EnsureIAMSystemTables(); err != nil {
		return nil, err
	}

	membership := &TenantMembership{}
	err := app.NonconcurrentDB().Select("*").
		From("_tenant_memberships").
		Where(dbx.HashExp{
			"tenant_id": tenantId,
			"user_id":   userId,
		}).
		One(membership)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMembershipNotFound
		}
		return nil, err
	}

	return membership, nil
}

// ListUserTenants returns all active tenants for a given user.
func (app *BaseApp) ListUserTenants(userId string) ([]*Tenant, error) {
	if err := app.EnsureIAMSystemTables(); err != nil {
		return nil, err
	}

	var tenants []*Tenant
	err := app.NonconcurrentDB().Select("t.*").
		From("_tenants t").
		InnerJoin("_tenant_memberships m", dbx.NewExp("m.tenant_id = t.id")).
		Where(dbx.HashExp{
			"m.user_id": userId,
			"m.status":  string(MembershipStatusActive),
			"t.status":  string(TenantStatusActive),
		}).
		OrderBy("t.created DESC").
		All(&tenants)

	if err != nil {
		return nil, err
	}

	return tenants, nil
}

// ValidateTenantAccess verifies that the given principal is authorized to access the specified tenant.
func (app *BaseApp) ValidateTenantAccess(p Principal, tenantId string) error {
	if tenantId == "" {
		return ErrTenantContextMissing
	}

	if p == nil || p.Type() == PrincipalTypeAnonymous {
		return ErrTenantAccessDenied
	}

	if p.Type() == PrincipalTypeSuperuser {
		return nil // superuser has global tenant access
	}

	// Verify tenant exists and is active
	tenant, err := app.GetTenant(tenantId)
	if err != nil {
		return err
	}

	if tenant.Status == TenantStatusSuspended {
		return ErrTenantSuspended
	}
	if tenant.Status == TenantStatusArchived {
		return ErrTenantArchived
	}
	if tenant.Status != TenantStatusActive {
		return ErrTenantAccessDenied
	}

	// For service accounts, verify tenant is in allowed list
	if p.Type() == PrincipalTypeService {
		if !p.InTenant(tenantId) {
			return ErrTenantAccessDenied
		}
		return nil
	}

	// For human users, verify active membership in database
	membership, err := app.GetTenantMembership(tenantId, p.Id())
	if err != nil || membership.Status != MembershipStatusActive {
		return ErrTenantAccessDenied
	}

	return nil
}
