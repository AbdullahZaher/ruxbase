package core

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/golang-jwt/jwt/v5"
	"github.com/pocketbase/dbx"
)

var (
	ErrInvalidToken   = errors.New("invalid or expired token")
	ErrSessionRevoked = errors.New("session has been revoked")
)

const (
	TokenClaimTenantId = "tenantId"
	TokenClaimRole     = "role"
)

// GenerateTenantAuthToken generates a signed JWT token bound to a user record and optional active tenant.
func (app *BaseApp) GenerateTenantAuthToken(record *Record, tenantId string, duration time.Duration) (string, error) {
	if record == nil {
		return "", errors.New("cannot generate token for nil record")
	}
	if !record.Collection().IsAuth() {
		return "", ErrNotAuthRecord
	}

	key := (record.TokenKey() + record.Collection().AuthToken.Secret)
	if key == "" {
		return "", ErrMissingSigningKey
	}

	if duration <= 0 {
		duration = record.Collection().AuthToken.DurationTime()
	}

	claims := jwt.MapClaims{
		TokenClaimType:         TokenTypeAuth,
		TokenClaimId:           record.Id,
		TokenClaimCollectionId: record.Collection().Id,
		TokenClaimTenantId:     tenantId,
	}

	// If tenantId is specified, fetch the user's role to embed as informational claim
	if tenantId != "" {
		membership, err := app.GetTenantMembership(tenantId, record.Id)
		if err == nil && membership.Status == MembershipStatusActive {
			claims[TokenClaimRole] = membership.Role
		}
	}

	return security.NewJWT(claims, key, duration)
}

// VerifyTenantAuthToken verifies a JWT token and constructs an authoritative UserPrincipal with fresh database state.
func (app *BaseApp) VerifyTenantAuthToken(tokenStr string) (*UserPrincipal, *Record, error) {
	if strings.TrimSpace(tokenStr) == "" {
		return nil, nil, ErrInvalidToken
	}

	unverifiedClaims, err := security.ParseUnverifiedJWT(tokenStr)
	if err != nil {
		return nil, nil, ErrInvalidToken
	}

	id, _ := unverifiedClaims[TokenClaimId].(string)
	collectionId, _ := unverifiedClaims[TokenClaimCollectionId].(string)
	tenantId, _ := unverifiedClaims[TokenClaimTenantId].(string)

	if id == "" || collectionId == "" {
		return nil, nil, ErrInvalidToken
	}

	// 1. Fetch user record from database
	record, err := app.FindRecordById(collectionId, id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load user record: %w", err)
	}

	// 2. Cryptographic and tokenKey verification: instant revocation if tokenKey changed
	key := (record.TokenKey() + record.Collection().AuthToken.Secret)
	_, err = security.ParseJWT(tokenStr, key)
	if err != nil {
		return nil, nil, ErrSessionRevoked
	}

	// 3. Load user's active tenant memberships from database
	memberships, err := app.loadUserMemberships(record.Id)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load user memberships: %w", err)
	}

	// 4. If token contains tenantId, validate access and ensure role freshness
	if tenantId != "" {
		userPrincipal := NewUserPrincipal(record.Id, record.Collection().Id, tenantId, memberships)
		if err := app.ValidateTenantAccess(userPrincipal, tenantId); err != nil {
			return nil, nil, err
		}
		return userPrincipal, record, nil
	}

	userPrincipal := NewUserPrincipal(record.Id, record.Collection().Id, "", memberships)
	return userPrincipal, record, nil
}

// loadUserMemberships retrieves all active memberships for a user.
func (app *BaseApp) loadUserMemberships(userId string) ([]*TenantMembership, error) {
	if err := app.EnsureIAMSystemTables(); err != nil {
		return nil, err
	}

	var memberships []*TenantMembership
	err := app.NonconcurrentDB().Select("*").
		From("_tenant_memberships").
		Where(dbx.HashExp{
			"user_id": userId,
			"status":  string(MembershipStatusActive),
		}).
		All(&memberships)

	if err != nil {
		return nil, err
	}

	return memberships, nil
}

// RevokeUserSession rotates the user record's tokenKey, instantly invalidating all issued tokens.
func (app *BaseApp) RevokeUserSession(record *Record) error {
	if record == nil {
		return errors.New("cannot revoke session on nil record")
	}

	record.SetTokenKey(security.RandomString(30))
	return app.Save(record)
}

// ReservedClientClaims lists keys that must never be accepted from client payloads.
var ReservedClientClaims = []string{
	"@request",
	"tenantId",
	"tenant_id",
	"_tenantId",
	"role",
	"roles",
	"permission",
	"permissions",
	"isSuperuser",
	"is_superuser",
	"system",
}

// SanitizeClientPayload removes any reserved authorization claims from client request bodies.
func SanitizeClientPayload(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}

	sanitized := make(map[string]any, len(data))
	for k, v := range data {
		lower := strings.ToLower(k)
		isReserved := false
		for _, reserved := range ReservedClientClaims {
			if strings.EqualFold(lower, reserved) || strings.HasPrefix(lower, "@request") {
				isReserved = true
				break
			}
		}
		if !isReserved {
			sanitized[k] = v
		}
	}

	return sanitized
}
