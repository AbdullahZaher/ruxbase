package core

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

type ServiceAccountStatus string

const (
	ServiceAccountActive    ServiceAccountStatus = "active"
	ServiceAccountSuspended ServiceAccountStatus = "suspended"
	ServiceAccountRevoked   ServiceAccountStatus = "revoked"
)

var (
	ErrServiceAccountNotFound           = errors.New("service account not found")
	ErrInvalidApiKeyFormat              = errors.New("invalid API key format")
	ErrInvalidApiKey                    = errors.New("invalid or incorrect API key")
	ErrServiceAccountExpired            = errors.New("service account API key has expired")
	ErrServiceAccountRevoked            = errors.New("service account has been revoked")
	ErrServiceAccountSuspended          = errors.New("service account is suspended")
	ErrServiceAccountTenantUnauthorized = errors.New("service account is not authorized for requested tenant")
)

const (
	ServiceApiKeyPrefixHeader = "rx_svc_"
)

// ServiceAccount defines a machine identity for automated backend operations.
type ServiceAccount struct {
	Id             string                  `db:"id" json:"id"`
	Name           string                  `db:"name" json:"name"`
	TenantId       string                  `db:"tenant_id" json:"tenantId"`
	Description    string                  `db:"description" json:"description"`
	Scopes         types.JSONArray[string] `db:"scopes" json:"scopes"`
	AllowedTenants types.JSONArray[string] `db:"allowed_tenants" json:"allowedTenants"`
	ApiKeyHash     string                  `db:"api_key_hash" json:"-"`
	ApiKeyPrefix   string                  `db:"api_key_prefix" json:"apiKeyPrefix"`
	Status         ServiceAccountStatus    `db:"status" json:"status"`
	ExpiresAt      *types.DateTime         `db:"expires_at" json:"expiresAt"`
	LastUsedAt     *types.DateTime         `db:"last_used_at" json:"lastUsedAt"`
	Created        types.DateTime          `db:"created" json:"created"`
	Updated        types.DateTime          `db:"updated" json:"updated"`
}

// TableName returns the physical table name for ServiceAccounts.
func (s *ServiceAccount) TableName() string {
	return "_service_accounts"
}

// EnsureServiceAccountsSystemTable initializes the _service_accounts physical table.
func (app *BaseApp) EnsureServiceAccountsSystemTable() error {
	return app.RunInTransaction(func(txApp App) error {
		_, err := txApp.NonconcurrentDB().NewQuery(`
			CREATE TABLE IF NOT EXISTS _service_accounts (
				id VARCHAR(15) PRIMARY KEY,
				name VARCHAR(255) NOT NULL,
				tenant_id VARCHAR(15) NOT NULL DEFAULT '',
				description TEXT NOT NULL DEFAULT '',
				scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
				allowed_tenants JSONB NOT NULL DEFAULT '[]'::jsonb,
				api_key_hash VARCHAR(255) NOT NULL,
				api_key_prefix VARCHAR(16) NOT NULL,
				status VARCHAR(32) NOT NULL DEFAULT 'active',
				expires_at TIMESTAMPTZ,
				last_used_at TIMESTAMPTZ,
				created TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx__service_accounts_prefix ON _service_accounts (api_key_prefix);
			CREATE INDEX IF NOT EXISTS idx__service_accounts_tenant ON _service_accounts (tenant_id);
			CREATE INDEX IF NOT EXISTS idx__service_accounts_status ON _service_accounts (status);
		`).Execute()
		if err != nil {
			return fmt.Errorf("failed to ensure _service_accounts table: %w", err)
		}
		return nil
	})
}

// generateServiceKey generates a new prefixed raw API key and its SHA-256 hash.
func generateServiceKey() (rawKey, prefix, hash string) {
	prefix = security.RandomString(8)
	secret := security.RandomString(32)
	rawKey = fmt.Sprintf("%s%s_%s", ServiceApiKeyPrefixHeader, prefix, secret)

	sum := sha256.Sum256([]byte(rawKey))
	hash = hex.EncodeToString(sum[:])
	return rawKey, prefix, hash
}

// hashServiceKey calculates the SHA-256 hex digest of a raw API key.
func hashServiceKey(rawKey string) string {
	sum := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(sum[:])
}

// CreateServiceAccount registers a new service account and returns the one-time raw API key.
func (app *BaseApp) CreateServiceAccount(name, tenantId, description string, scopes, allowedTenants []string, expiresAt *time.Time) (*ServiceAccount, string, error) {
	if err := app.EnsureServiceAccountsSystemTable(); err != nil {
		return nil, "", err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", errors.New("service account name cannot be empty")
	}

	rawKey, prefix, hash := generateServiceKey()

	var expDT *types.DateTime
	if expiresAt != nil {
		dt, _ := types.ParseDateTime(*expiresAt)
		expDT = &dt
	}

	sa := &ServiceAccount{
		Id:             security.RandomString(15),
		Name:           name,
		TenantId:       tenantId,
		Description:    strings.TrimSpace(description),
		Scopes:         types.JSONArray[string](scopes),
		AllowedTenants: types.JSONArray[string](allowedTenants),
		ApiKeyHash:     hash,
		ApiKeyPrefix:   prefix,
		Status:         ServiceAccountActive,
		ExpiresAt:      expDT,
		Created:        types.NowDateTime(),
		Updated:        types.NowDateTime(),
	}

	err := app.NonconcurrentDB().Model(sa).Insert()
	if err != nil {
		return nil, "", fmt.Errorf("failed to insert service account: %w", err)
	}

	return sa, rawKey, nil
}

// GetServiceAccount retrieves a service account by its ID.
func (app *BaseApp) GetServiceAccount(id string) (*ServiceAccount, error) {
	if err := app.EnsureServiceAccountsSystemTable(); err != nil {
		return nil, err
	}

	sa := &ServiceAccount{}
	err := app.NonconcurrentDB().Select("*").
		From("_service_accounts").
		Where(dbx.HashExp{"id": id}).
		One(sa)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrServiceAccountNotFound
		}
		return nil, err
	}

	return sa, nil
}

// RotateServiceAccountKey rotates the API key for an active service account, invalidating the previous key.
func (app *BaseApp) RotateServiceAccountKey(id string) (string, error) {
	sa, err := app.GetServiceAccount(id)
	if err != nil {
		return "", err
	}

	if sa.Status == ServiceAccountRevoked {
		return "", ErrServiceAccountRevoked
	}

	rawKey, prefix, hash := generateServiceKey()

	sa.ApiKeyPrefix = prefix
	sa.ApiKeyHash = hash
	sa.Updated = types.NowDateTime()

	if err := app.NonconcurrentDB().Model(sa).Update(); err != nil {
		return "", fmt.Errorf("failed to rotate service account key: %w", err)
	}

	return rawKey, nil
}

// RevokeServiceAccount permanently marks a service account as revoked.
func (app *BaseApp) RevokeServiceAccount(id string) error {
	sa, err := app.GetServiceAccount(id)
	if err != nil {
		return err
	}

	sa.Status = ServiceAccountRevoked
	sa.Updated = types.NowDateTime()

	return app.NonconcurrentDB().Model(sa).Update()
}

// AuthenticateServiceAccount validates a raw API key, enforces expiration and tenant boundaries,
// and constructs an authorized ServicePrincipal.
func (app *BaseApp) AuthenticateServiceAccount(rawKey, requestedTenantId string) (*ServicePrincipal, error) {
	if err := app.EnsureServiceAccountsSystemTable(); err != nil {
		return nil, err
	}

	rawKey = strings.TrimSpace(rawKey)
	if !strings.HasPrefix(rawKey, ServiceApiKeyPrefixHeader) {
		return nil, ErrInvalidApiKeyFormat
	}

	// Key format: rx_svc_<prefix>_<secret>
	trimmed := strings.TrimPrefix(rawKey, ServiceApiKeyPrefixHeader)
	parts := strings.SplitN(trimmed, "_", 2)
	if len(parts) != 2 || len(parts[0]) != 8 {
		return nil, ErrInvalidApiKeyFormat
	}

	prefix := parts[0]

	// Lookup service account by prefix
	sa := &ServiceAccount{}
	err := app.NonconcurrentDB().Select("*").
		From("_service_accounts").
		Where(dbx.HashExp{"api_key_prefix": prefix}).
		One(sa)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvalidApiKey
		}
		return nil, err
	}

	// Constant-time hash verification
	incomingHash := hashServiceKey(rawKey)
	if subtle.ConstantTimeCompare([]byte(sa.ApiKeyHash), []byte(incomingHash)) != 1 {
		return nil, ErrInvalidApiKey
	}

	// Status checks
	if sa.Status == ServiceAccountRevoked {
		return nil, ErrServiceAccountRevoked
	}
	if sa.Status == ServiceAccountSuspended {
		return nil, ErrServiceAccountSuspended
	}

	// Expiration check
	if sa.ExpiresAt != nil && !sa.ExpiresAt.IsZero() {
		if time.Now().After(sa.ExpiresAt.Time()) {
			return nil, ErrServiceAccountExpired
		}
	}

	// Tenant boundary validation
	activeTenant := sa.TenantId
	if requestedTenantId != "" {
		if sa.TenantId != "" && sa.TenantId != requestedTenantId {
			return nil, ErrServiceAccountTenantUnauthorized
		}
		if len(sa.AllowedTenants) > 0 && !slices.Contains(sa.AllowedTenants, "*") && !slices.Contains(sa.AllowedTenants, requestedTenantId) {
			return nil, ErrServiceAccountTenantUnauthorized
		}
		activeTenant = requestedTenantId
	}

	// Update last_used_at in background or non-blocking
	now := types.NowDateTime()
	sa.LastUsedAt = &now
	_ = app.NonconcurrentDB().Model(sa).Update("LastUsedAt")

	principal := NewServicePrincipal(
		sa.Id,
		activeTenant,
		[]string(sa.Scopes),
		[]string(sa.AllowedTenants),
		[]string{"service"},
	)

	return principal, nil
}
