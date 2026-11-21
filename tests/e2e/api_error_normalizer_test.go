package e2e_test

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestAPIError_DomainMappings(t *testing.T) {
	testCases := []struct {
		name         string
		err          error
		expectedCode int
		expectedSlug string
	}{
		{
			name:         "Tenant Access Denied",
			err:          core.ErrTenantAccessDenied,
			expectedCode: http.StatusForbidden,
			expectedSlug: "tenant_access_denied",
		},
		{
			name:         "Tenant Suspended",
			err:          core.ErrTenantSuspended,
			expectedCode: http.StatusForbidden,
			expectedSlug: "tenant_suspended",
		},
		{
			name:         "Tenant Not Found",
			err:          core.ErrTenantNotFound,
			expectedCode: http.StatusNotFound,
			expectedSlug: "tenant_not_found",
		},
		{
			name:         "Cross Tenant Relation Forbidden",
			err:          core.ErrCrossTenantRelationForbidden,
			expectedCode: http.StatusBadRequest,
			expectedSlug: "cross_tenant_relation_forbidden",
		},
		{
			name:         "Tenant Mismatch",
			err:          core.ErrTenantMismatch,
			expectedCode: http.StatusForbidden,
			expectedSlug: "tenant_mismatch",
		},
		{
			name:         "Tenant File Access Denied",
			err:          core.ErrTenantFileAccessDenied,
			expectedCode: http.StatusForbidden,
			expectedSlug: "tenant_file_access_denied",
		},
		{
			name:         "Service Account Expired",
			err:          core.ErrServiceAccountExpired,
			expectedCode: http.StatusUnauthorized,
			expectedSlug: "service_account_expired",
		},
		{
			name:         "Service Account Revoked",
			err:          core.ErrServiceAccountRevoked,
			expectedCode: http.StatusUnauthorized,
			expectedSlug: "service_account_revoked",
		},
		{
			name:         "Role Not Found",
			err:          core.ErrRoleNotFound,
			expectedCode: http.StatusNotFound,
			expectedSlug: "role_not_found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := apis.NormalizeDatabaseError(tc.err)
			if apiErr == nil {
				t.Fatalf("Expected ApiError, got nil")
			}
			if apiErr.Status != tc.expectedCode {
				t.Errorf("Expected status %d, got %d", tc.expectedCode, apiErr.Status)
			}
			data := apiErr.Data
			if data["code"] != tc.expectedSlug {
				t.Errorf("Expected slug %q, got %v", tc.expectedSlug, data["code"])
			}
		})
	}
}

func TestAPIError_SQLSTATEMappings(t *testing.T) {
	testCases := []struct {
		name         string
		err          error
		expectedCode int
		expectedSlug string
	}{
		{
			name:         "RLS Policy Violation (42501)",
			err:          errors.New("ERROR: new row violates row-level security policy for table 'projects' (SQLSTATE 42501)"),
			expectedCode: http.StatusForbidden,
			expectedSlug: "rls_policy_violation",
		},
		{
			name:         "Unique Constraint Violation (23505)",
			err:          errors.New("ERROR: duplicate key value violates unique constraint 'uq_email' (SQLSTATE 23505)"),
			expectedCode: http.StatusBadRequest,
			expectedSlug: "unique_constraint_violation",
		},
		{
			name:         "Foreign Key Violation (23503)",
			err:          errors.New("ERROR: insert or update on table violates foreign key constraint (SQLSTATE 23503)"),
			expectedCode: http.StatusBadRequest,
			expectedSlug: "foreign_key_violation",
		},
		{
			name:         "Check Constraint Violation (23514)",
			err:          errors.New("ERROR: new row violates check constraint 'check_balance' (SQLSTATE 23514)"),
			expectedCode: http.StatusBadRequest,
			expectedSlug: "check_constraint_violation",
		},
		{
			name:         "Undefined Table (42P01)",
			err:          errors.New("ERROR: relation 'unknown_table' does not exist (SQLSTATE 42P01)"),
			expectedCode: http.StatusNotFound,
			expectedSlug: "resource_not_found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			apiErr := apis.NormalizeDatabaseError(tc.err)
			if apiErr == nil {
				t.Fatalf("Expected ApiError, got nil")
			}
			if apiErr.Status != tc.expectedCode {
				t.Errorf("Expected status %d, got %d", tc.expectedCode, apiErr.Status)
			}
			data := apiErr.Data
			if data["code"] != tc.expectedSlug {
				t.Errorf("Expected slug %q, got %v", tc.expectedSlug, data["code"])
			}
		})
	}
}

func TestAPIError_CredentialSanitization(t *testing.T) {
	leakedErr := errors.New("connection failed: postgres://db_admin:supersecretpassword@10.0.0.5:5432/ruxbase_prod: timeout connecting to server")
	sanitized := apis.SanitizeErrorString(leakedErr.Error())

	if sanitized == leakedErr.Error() {
		t.Fatalf("Sanitization failed to modify leaked error string")
	}
	if fmt.Sprint(sanitized) != "connection failed: [REDACTED_DATABASE_URL] timeout connecting to server" {
		t.Errorf("Unexpected sanitized result: %q", sanitized)
	}
}

func TestAPIError_ServiceAccountPrincipalResolution(t *testing.T) {
	testApp, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer testApp.Cleanup()

	// 1. Create a service account
	sa, rawKey, err := testApp.CreateServiceAccount(
		"Pipeline Bot",
		"",
		"Automation bot",
		[]string{"records.read"},
		[]string{"*"},
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to create service account: %v", err)
	}

	// 2. Simulate HTTP Request with Authorization Header
	req := httptest.NewRequest(http.MethodGet, "/api/records?tenant=t_abc", nil)
	req.Header.Set("Authorization", "Bearer "+rawKey)

	event := &core.RequestEvent{
		Request: req,
		App:     testApp,
	}

	// Extract principal via PrincipalFromEvent after service auth
	sp, err := testApp.AuthenticateServiceAccount(rawKey, "t_abc")
	if err != nil {
		t.Fatalf("Failed to authenticate service account: %v", err)
	}
	event.Set("principal", sp)

	resolved := apis.PrincipalFromEvent(event)
	if resolved == nil {
		t.Fatalf("Expected non-nil resolved principal")
	}
	if resolved.Id() != sa.Id {
		t.Errorf("Expected resolved principal ID %s, got %s", sa.Id, resolved.Id())
	}
	if resolved.Type() != core.PrincipalTypeService {
		t.Errorf("Expected type 'service', got %s", resolved.Type())
	}
	if resolved.TenantId() != "t_abc" {
		t.Errorf("Expected tenant 't_abc', got %s", resolved.TenantId())
	}
}
