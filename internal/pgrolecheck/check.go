// Package pgrolecheck performs read-only admission diagnostics for a proposed
// PostgreSQL serving identity. It is not an authorization engine.
package pgrolecheck

import (
	"context"
	"database/sql"
	"errors"
)

// Report records a conservative subset of runtime authority. Membership is
// treated as a reason for review even when inheritance or SET is disabled.
type Report struct {
	SessionRole      string `json:"sessionRole"`
	CurrentRole      string `json:"currentRole"`
	PrivilegedMember bool   `json:"privilegedMember"`
	DatabaseOwner    bool   `json:"databaseOwner"`
	DatabaseCreate   bool   `json:"databaseCreate"`
	SchemaOwner      bool   `json:"schemaOwner"`
	SchemaCreate     bool   `json:"schemaCreate"`
	RelationOwner    bool   `json:"relationOwner"`
	RoutineOwner     bool   `json:"routineOwner"`
}

// Findings lists reasons the proposed serving identity fails this initial
// authority screen. An empty list does not establish complete least privilege.
func (r Report) Findings() []string {
	result := []string{}
	if r.SessionRole == "" || r.CurrentRole == "" || r.SessionRole != r.CurrentRole {
		result = append(result, "session_identity_mismatch")
	}
	if r.PrivilegedMember {
		result = append(result, "privileged_role_membership")
	}
	if r.DatabaseOwner {
		result = append(result, "database_ownership")
	}
	if r.DatabaseCreate {
		result = append(result, "database_create")
	}
	if r.SchemaOwner {
		result = append(result, "schema_ownership")
	}
	if r.SchemaCreate {
		result = append(result, "schema_create")
	}
	if r.RelationOwner {
		result = append(result, "relation_ownership")
	}
	if r.RoutineOwner {
		result = append(result, "routine_ownership")
	}
	return result
}

// Inspect uses a read-only transaction with no bootstrap or DDL. The caller
// owns the pool and supplies a bounded context. Errors are deliberately
// generic so connection strings are not accidentally printed by callers.
func Inspect(ctx context.Context, db *sql.DB) (Report, error) {
	if db == nil {
		return Report{}, errors.New("missing PostgreSQL connection pool")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return Report{}, errors.New("unable to begin read-only role inspection")
	}
	defer func() { _ = tx.Rollback() }()
	var report Report
	err = tx.QueryRowContext(ctx, authorityQuery).Scan(
		&report.SessionRole, &report.CurrentRole, &report.PrivilegedMember,
		&report.DatabaseOwner, &report.DatabaseCreate, &report.SchemaOwner,
		&report.SchemaCreate, &report.RelationOwner, &report.RoutineOwner,
	)
	if err != nil {
		return Report{}, errors.New("unable to inspect PostgreSQL role authority")
	}
	if err := tx.Commit(); err != nil {
		return Report{}, errors.New("unable to finish read-only role inspection")
	}
	return report, nil
}

const authorityQuery = `
SELECT session_user, current_user,
       EXISTS (
           SELECT 1 FROM pg_catalog.pg_roles r
           WHERE (r.rolname = session_user OR pg_catalog.pg_has_role(session_user, r.oid, 'MEMBER'))
             AND (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole
                  OR r.rolreplication OR r.rolbypassrls)
       ),
       pg_catalog.pg_has_role(session_user, d.datdba, 'MEMBER'),
       pg_catalog.has_database_privilege(session_user, d.oid, 'CREATE'),
       EXISTS (
           SELECT 1 FROM pg_catalog.pg_namespace n
           WHERE n.nspname <> 'information_schema' AND pg_catalog.left(n.nspname, 3) <> 'pg_'
             AND pg_catalog.pg_has_role(session_user, n.nspowner, 'MEMBER')
       ),
       EXISTS (
           SELECT 1 FROM pg_catalog.pg_namespace n
           WHERE n.nspname <> 'information_schema' AND pg_catalog.left(n.nspname, 3) <> 'pg_'
             AND pg_catalog.has_schema_privilege(session_user, n.oid, 'CREATE')
       ),
       EXISTS (
           SELECT 1 FROM pg_catalog.pg_class c
           JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
           WHERE n.nspname <> 'information_schema' AND pg_catalog.left(n.nspname, 3) <> 'pg_'
             AND pg_catalog.pg_has_role(session_user, c.relowner, 'MEMBER')
       ),
       EXISTS (
           SELECT 1 FROM pg_catalog.pg_proc p
           JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
           WHERE n.nspname <> 'information_schema' AND pg_catalog.left(n.nspname, 3) <> 'pg_'
             AND pg_catalog.pg_has_role(session_user, p.proowner, 'MEMBER')
       )
FROM pg_catalog.pg_database d
WHERE d.datname = pg_catalog.current_database()
`
