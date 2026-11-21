package e2e_test

import (
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestSecurityHardening_SecurityDefinerGuard(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a function marked with SECURITY DEFINER
	setupSQL := `
		CREATE OR REPLACE FUNCTION admin_privileged_reset(param text)
		RETURNS text LANGUAGE sql SECURITY DEFINER AS 'SELECT ''reset_'' || param';
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create security definer function: %v", err)
	}

	// 2. Negative check: attempt to expose SECURITY DEFINER function publicly without opt-in
	publicRule := ""
	_, err = app.ExposeRPCFunction(core.ExposeFunctionOptions{
		FunctionName:         "admin_privileged_reset",
		AccessRule:           &publicRule,
		AllowSecurityDefiner: false,
	})
	if err == nil {
		t.Fatalf("Expected public exposure of SECURITY DEFINER function without opt-in to fail, but succeeded")
	}
	if !strings.Contains(err.Error(), "SECURITY DEFINER") {
		t.Errorf("Expected SECURITY DEFINER error message, got: %v", err)
	}

	// 3. Positive check: expose with explicit AllowSecurityDefiner opt-in
	fn, err := app.ExposeRPCFunction(core.ExposeFunctionOptions{
		FunctionName:         "admin_privileged_reset",
		AccessRule:           &publicRule,
		AllowSecurityDefiner: true,
	})
	if err != nil {
		t.Fatalf("Expected exposure with AllowSecurityDefiner=true to succeed, but failed: %v", err)
	}
	if !fn.SecurityDefiner {
		t.Errorf("Expected function to be flagged as SecurityDefiner")
	}
}

func TestSecurityHardening_DangerousBuiltinRejection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Attempt to expose pg_read_file
	publicRule := ""
	_, err = app.ExposeRPCFunction(core.ExposeFunctionOptions{
		FunctionName: "pg_read_file",
		AccessRule:   &publicRule,
	})
	if err == nil {
		t.Fatalf("Expected exposing pg_read_file to fail, but succeeded")
	}
	if !strings.Contains(err.Error(), "blacklisted") {
		t.Errorf("Expected blacklisted function error message, got: %v", err)
	}

	// 2. Attempt to execute pg_terminate_backend directly via RPC
	_, err = app.ExecuteRPC("pg_terminate_backend", map[string]any{"pid": 1234}, nil, true)
	if err == nil {
		t.Fatalf("Expected executing pg_terminate_backend to fail, but succeeded")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("Expected blocked error message, got: %v", err)
	}
}

func TestSecurityHardening_RoutineSecurityAudit(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a view without security_invoker = true
	if _, err := app.DB().NewQuery("CREATE VIEW uninvoked_insecure_view AS SELECT 1 AS num;").Execute(); err != nil {
		t.Fatalf("Failed to create view: %v", err)
	}

	// 2. Run security audit
	report, err := app.AuditDatabaseRoutineSecurity("public")
	if err != nil {
		t.Fatalf("AuditDatabaseRoutineSecurity failed: %v", err)
	}

	// Verify insecure view was caught
	var foundInsecureView bool
	for _, v := range report.MissingSecurityInvokerViews {
		if v == "uninvoked_insecure_view" {
			foundInsecureView = true
			break
		}
	}
	if !foundInsecureView {
		t.Errorf("Expected uninvoked_insecure_view in MissingSecurityInvokerViews: %v", report.MissingSecurityInvokerViews)
	}
	if report.IsSecure {
		t.Errorf("Expected IsSecure to be false when uninvoked view exists")
	}

	// 3. Remediate view by setting security_invoker = true
	if _, err := app.DB().NewQuery("ALTER VIEW uninvoked_insecure_view SET (security_invoker = true);").Execute(); err != nil {
		t.Fatalf("Failed to alter view: %v", err)
	}

	// 4. Re-run audit and assert remediation
	remediatedReport, err := app.AuditDatabaseRoutineSecurity("public")
	if err != nil {
		t.Fatalf("AuditDatabaseRoutineSecurity after remediation failed: %v", err)
	}

	for _, v := range remediatedReport.MissingSecurityInvokerViews {
		if v == "uninvoked_insecure_view" {
			t.Errorf("Remediated view still reported in MissingSecurityInvokerViews")
		}
	}
	if !remediatedReport.IsSecure {
		t.Errorf("Expected IsSecure to be true after view remediation: %v", remediatedReport)
	}
}
