package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
)

// DangerousBuiltinFunctions lists PostgreSQL administrative and filesystem functions
// that are strictly forbidden from exposure or RPC invocation.
var DangerousBuiltinFunctions = map[string]bool{
	"pg_read_file":           true,
	"pg_read_binary_file":    true,
	"pg_write_file":          true,
	"pg_read_file_old":       true,
	"pg_terminate_backend":   true,
	"pg_cancel_backend":      true,
	"pg_reload_conf":         true,
	"pg_rotate_logfile":      true,
	"lo_import":              true,
	"lo_export":              true,
	"lo_unlink":              true,
	"dblink":                 true,
	"dblink_exec":            true,
	"dblink_connect":         true,
	"dblink_disconnect":      true,
}

// RoutineSecurityAuditReport details findings from a security review of database routines and views.
type RoutineSecurityAuditReport struct {
	CheckedAt                    time.Time `json:"checkedAt"`
	Schema                       string    `json:"schema"`
	SecurityDefinerFunctions     []string  `json:"securityDefinerFunctions"`
	MissingSecurityInvokerViews  []string  `json:"missingSecurityInvokerViews"`
	DangerousRoutinesFound       []string  `json:"dangerousRoutinesFound"`
	IsSecure                     bool      `json:"isSecure"`
}

// AuditDatabaseRoutineSecurity inspects PostgreSQL catalogs to detect potential privilege escalation
// or RLS bypass risks in stored procedures and database views.
func (app *BaseApp) AuditDatabaseRoutineSecurity(schema string) (*RoutineSecurityAuditReport, error) {
	if schema == "" {
		schema = "public"
	}

	report := &RoutineSecurityAuditReport{
		CheckedAt:                   time.Now(),
		Schema:                      schema,
		SecurityDefinerFunctions:    []string{},
		MissingSecurityInvokerViews: []string{},
		DangerousRoutinesFound:      []string{},
		IsSecure:                    true,
	}

	// 1. Audit SECURITY DEFINER functions
	var secDefinerFuncs []string
	err := app.ConcurrentDB().NewQuery(`
		SELECT p.[[proname]]
		FROM {{pg_proc}} p
		JOIN {{pg_namespace}} n ON n.[[oid]] = p.[[pronamespace]]
		WHERE n.[[nspname]] = {:schema}
			AND p.[[prosecdef]] = true
			AND NOT starts_with(p.[[proname]], 'pg_')
		ORDER BY p.[[proname]]
	`).Bind(dbx.Params{"schema": schema}).Column(&secDefinerFuncs)
	if err != nil {
		return nil, fmt.Errorf("failed to audit SECURITY DEFINER functions: %w", err)
	}
	report.SecurityDefinerFunctions = secDefinerFuncs

	// 2. Audit views lacking security_invoker = true
	var uninvokedViews []string
	err = app.ConcurrentDB().NewQuery(`
		SELECT c.[[relname]]
		FROM {{pg_class}} c
		JOIN {{pg_namespace}} n ON n.[[oid]] = c.[[relnamespace]]
		WHERE n.[[nspname]] = {:schema}
			AND c.[[relkind]] = 'v'
			AND NOT starts_with(c.[[relname]], '_')
			AND NOT starts_with(c.[[relname]], 'pg_')
			AND (c.[[reloptions]] IS NULL OR NOT c.[[reloptions]]::text LIKE '%security_invoker=true%')
		ORDER BY c.[[relname]]
	`).Bind(dbx.Params{"schema": schema}).Column(&uninvokedViews)
	if err != nil {
		return nil, fmt.Errorf("failed to audit view security options: %w", err)
	}
	report.MissingSecurityInvokerViews = uninvokedViews

	// 3. Check for any dangerous built-ins exposed or present in user schema
	for _, fn := range secDefinerFuncs {
		if DangerousBuiltinFunctions[strings.ToLower(fn)] {
			report.DangerousRoutinesFound = append(report.DangerousRoutinesFound, fn)
		}
	}

	// Security evaluation: Missing security_invoker views or dangerous routines compromise tenant/RLS boundaries
	if len(report.MissingSecurityInvokerViews) > 0 || len(report.DangerousRoutinesFound) > 0 {
		report.IsSecure = false
	}

	return report, nil
}
