package apis

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tools/router"
)

// ErrorCode represents a machine-readable error slug that implements router.SafeErrorResolver.
type ErrorCode string

func (c ErrorCode) Code() string {
	return string(c)
}

func (c ErrorCode) Error() string {
	return string(c)
}

func (c ErrorCode) Resolve(errData map[string]any) any {
	return string(c)
}

func codeData(code string, extra ...map[string]any) map[string]any {
	res := map[string]any{
		"code": ErrorCode(code),
	}
	for _, m := range extra {
		for k, v := range m {
			res[k] = ErrorCode(fmt.Sprint(v))
		}
	}
	return res
}

// NormalizeDatabaseError translates internal database errors, SQLSTATE codes,
// RLS policy violations, and IAM domain errors into standardized, structured ApiErrors.
func NormalizeDatabaseError(err error) *router.ApiError {
	if err == nil {
		return nil
	}

	// 1. If already an ApiError, return as-is
	var apiErr *router.ApiError
	if errors.As(err, &apiErr) {
		return apiErr
	}

	// 2. IAM & Tenancy specific domain errors
	if errors.Is(err, core.ErrTenantAccessDenied) {
		return router.NewApiError(http.StatusForbidden, "Access to tenant denied", codeData("tenant_access_denied"))
	}
	if errors.Is(err, core.ErrTenantSuspended) {
		return router.NewApiError(http.StatusForbidden, "Tenant is suspended", codeData("tenant_suspended"))
	}
	if errors.Is(err, core.ErrTenantArchived) {
		return router.NewApiError(http.StatusForbidden, "Tenant is archived", codeData("tenant_archived"))
	}
	if errors.Is(err, core.ErrTenantNotFound) {
		return router.NewApiError(http.StatusNotFound, "Tenant not found", codeData("tenant_not_found"))
	}
	if errors.Is(err, core.ErrTenantMismatch) {
		return router.NewApiError(http.StatusForbidden, "Record tenant does not match active principal tenant", codeData("tenant_mismatch"))
	}
	if errors.Is(err, core.ErrCrossTenantRelationForbidden) {
		return router.NewApiError(http.StatusBadRequest, "Cross-tenant relational linking is forbidden", codeData("cross_tenant_relation_forbidden"))
	}
	if errors.Is(err, core.ErrTenantFileAccessDenied) {
		return router.NewApiError(http.StatusForbidden, "Access to tenant file denied", codeData("tenant_file_access_denied"))
	}
	if errors.Is(err, core.ErrServiceAccountExpired) {
		return router.NewApiError(http.StatusUnauthorized, "Service account API key has expired", codeData("service_account_expired"))
	}
	if errors.Is(err, core.ErrServiceAccountRevoked) {
		return router.NewApiError(http.StatusUnauthorized, "Service account has been revoked", codeData("service_account_revoked"))
	}
	if errors.Is(err, core.ErrServiceAccountSuspended) {
		return router.NewApiError(http.StatusForbidden, "Service account is suspended", codeData("service_account_suspended"))
	}
	if errors.Is(err, core.ErrServiceAccountTenantUnauthorized) {
		return router.NewApiError(http.StatusForbidden, "Service account is not authorized for requested tenant", codeData("service_account_tenant_unauthorized"))
	}
	if errors.Is(err, core.ErrRoleNotFound) {
		return router.NewApiError(http.StatusNotFound, "Role not found", codeData("role_not_found"))
	}
	if errors.Is(err, core.ErrRoleAlreadyExists) {
		return router.NewApiError(http.StatusBadRequest, "Role already exists", codeData("role_already_exists"))
	}
	if errors.Is(err, core.ErrSystemRoleImmutable) {
		return router.NewApiError(http.StatusBadRequest, "System role cannot be modified or deleted", codeData("system_role_immutable"))
	}

	// 3. PostgreSQL SQLSTATE errors & string patterns
	errStr := err.Error()

	// 42501: Insufficient privilege / RLS policy violation
	if strings.Contains(errStr, "42501") || strings.Contains(errStr, "violates row-level security policy") {
		return router.NewApiError(http.StatusForbidden, "Operation denied by row-level security policy", codeData("rls_policy_violation", map[string]any{"sqlstate": "42501"}))
	}

	// 23505: Unique constraint violation
	if strings.Contains(errStr, "23505") || strings.Contains(errStr, "duplicate key") {
		return router.NewApiError(http.StatusBadRequest, "Unique constraint violation", codeData("unique_constraint_violation", map[string]any{"sqlstate": "23505"}))
	}

	// 23503: Foreign key violation
	if strings.Contains(errStr, "23503") || strings.Contains(errStr, "foreign key constraint") {
		return router.NewApiError(http.StatusBadRequest, "Foreign key constraint violation", codeData("foreign_key_violation", map[string]any{"sqlstate": "23503"}))
	}

	// 23514: Check constraint violation
	if strings.Contains(errStr, "23514") || strings.Contains(errStr, "check constraint") {
		return router.NewApiError(http.StatusBadRequest, "Check constraint violation", codeData("check_constraint_violation", map[string]any{"sqlstate": "23514"}))
	}

	// 42P01: Undefined table / resource
	if strings.Contains(errStr, "42P01") || strings.Contains(errStr, "does not exist") {
		return router.NewApiError(http.StatusNotFound, "Requested resource or table does not exist", codeData("resource_not_found", map[string]any{"sqlstate": "42P01"}))
	}

	// 4. Default sanitized fallback: remove database URLs, secrets, or internal hostnames
	cleanMsg := SanitizeErrorString(errStr)
	return router.NewApiError(http.StatusBadRequest, "Database operation failed: "+cleanMsg, codeData("database_error"))
}

// SanitizeErrorString removes connection strings, passwords, and sensitive URLs from error text.
func SanitizeErrorString(msg string) string {
	if idx := strings.Index(msg, "postgres://"); idx != -1 {
		endIdx := strings.Index(msg[idx:], " ")
		if endIdx == -1 {
			msg = msg[:idx] + "[REDACTED_DATABASE_URL]"
		} else {
			msg = msg[:idx] + "[REDACTED_DATABASE_URL]" + msg[idx+endIdx:]
		}
	}
	return strings.TrimSpace(msg)
}

// PrincipalFromEvent extracts the active Principal from a RequestEvent.
func PrincipalFromEvent(e *core.RequestEvent) core.Principal {
	if e == nil {
		return &core.AnonymousPrincipal{}
	}

	if p, ok := e.Get("principal").(core.Principal); ok && p != nil {
		return p
	}

	if e.Auth != nil {
		if e.Auth.IsSuperuser() {
			return core.NewSuperuserPrincipal(e.Auth.Id, "")
		}
		activeTenant := e.Request.Header.Get("X-Tenant-Id")
		return core.NewUserPrincipal(e.Auth.Id, e.Auth.Collection().Name, activeTenant, nil)
	}

	return &core.AnonymousPrincipal{}
}
