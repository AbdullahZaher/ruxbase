package pgrolecheck

import (
	"context"
	"database/sql"
	"errors"
)

// CheckSeparation verifies that the actual declared roles exist, are distinct,
// and that the service and migration principals cannot inherit or assume
// another declared principal's authority. This is a read-only admission
// check, not a replacement for explicit ACLs or provisioning.
func (c IdentityContract) CheckSeparation(ctx context.Context, db *sql.DB, expected string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if db == nil || (expected != c.Runtime && expected != c.Migration && expected != c.Provisioning && (c.Replication == "" || expected != c.Replication)) {
		return errors.New("explicit declared identity and connection required")
	}
	names := []string{c.Runtime, c.Migration, c.Provisioning}
	if c.Replication != "" {
		names = append(names, c.Replication)
	}
	var ready bool
	err := db.QueryRowContext(ctx, `
SELECT session_user = $1 AND current_user = $1
  AND (SELECT count(*) FROM pg_catalog.pg_roles WHERE rolname = ANY($2::text[])) = cardinality($2::text[])
  AND NOT EXISTS (
      SELECT 1 FROM pg_catalog.pg_roles a
      CROSS JOIN pg_catalog.pg_roles b
      WHERE a.rolname = ANY($2::text[]) AND a.rolname <> $3
        AND b.rolname = ANY($2::text[]) AND a.oid <> b.oid
        AND pg_catalog.pg_has_role(a.oid, b.oid, 'MEMBER')
  )
  AND NOT EXISTS (
      SELECT 1 FROM pg_catalog.pg_roles a
      WHERE a.rolname IN ($4, $5)
        AND (NOT a.rolcanlogin OR a.rolsuper OR a.rolcreatedb OR a.rolcreaterole
             OR a.rolreplication OR a.rolbypassrls)
  )
  AND ($6 = '' OR EXISTS (
      SELECT 1 FROM pg_catalog.pg_roles a
      WHERE a.rolname = $6 AND a.rolcanlogin AND a.rolreplication
        AND NOT (a.rolsuper OR a.rolcreatedb OR a.rolcreaterole OR a.rolbypassrls)
  ))
`, expected, names, c.Provisioning, c.Runtime, c.Migration, c.Replication).Scan(&ready)
	if err != nil || !ready {
		return errors.New("declared database identity separation rejected")
	}
	return nil
}
