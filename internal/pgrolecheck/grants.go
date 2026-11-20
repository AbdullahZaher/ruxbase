package pgrolecheck

import (
	"context"
	"database/sql"
	"errors"
	"sort"
)

// Grant describes an effective privilege in a single reviewed application
// schema. Grant options are separate privileges with the WITH GRANT OPTION
// suffix. Extended contracts also include effective column and routine ACLs.
type Grant struct {
	Kind      string `json:"kind"`
	Object    string `json:"object"`
	Privilege string `json:"privilege"`
}

// GrantContract declares the exact effective grants for one runtime identity
// in a named application schema. No wildcard or implicit grant is accepted.
type GrantContract struct {
	Role     string  `json:"role"`
	Schema   string  `json:"schema"`
	Grants   []Grant `json:"grants"`
	Extended bool    `json:"extended,omitempty"`
}

// GrantDrift separates missing grants from extra authority.
type GrantDrift struct {
	Missing    []Grant `json:"missing"`
	Unexpected []Grant `json:"unexpected"`
}

func grantLess(a, b Grant) bool {
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	if a.Object != b.Object {
		return a.Object < b.Object
	}
	return a.Privilege < b.Privilege
}

// CompareGrants compares effective grants as sets, with deterministic output.
func CompareGrants(expected, actual []Grant) GrantDrift {
	want := make(map[Grant]bool, len(expected))
	got := make(map[Grant]bool, len(actual))
	for _, g := range expected {
		want[g] = true
	}
	for _, g := range actual {
		got[g] = true
	}
	drift := GrantDrift{Missing: []Grant{}, Unexpected: []Grant{}}
	for g := range want {
		if !got[g] {
			drift.Missing = append(drift.Missing, g)
		}
	}
	for g := range got {
		if !want[g] {
			drift.Unexpected = append(drift.Unexpected, g)
		}
	}
	sort.Slice(drift.Missing, func(i, j int) bool { return grantLess(drift.Missing[i], drift.Missing[j]) })
	sort.Slice(drift.Unexpected, func(i, j int) bool { return grantLess(drift.Unexpected[i], drift.Unexpected[j]) })
	return drift
}

// Check inspects grants on the actual session identity in a read-only catalog
// transaction. It reports drift and fails on unexpected or missing grants.
// Extended mode includes effective column grants and routine execution.
// Neither mode certifies other schemas, ownership, RLS, role switching,
// future privileges or the serving pool.
func (c GrantContract) Check(ctx context.Context, db *sql.DB) (GrantDrift, error) {
	if c.Role == "" || c.Schema == "" || c.Grants == nil || db == nil {
		return GrantDrift{}, errors.New("incomplete scoped grant contract")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return GrantDrift{}, errors.New("unable to begin scoped grant inspection")
	}
	defer func() { _ = tx.Rollback() }()
	drift, err := c.checkTx(ctx, tx)
	if err != nil {
		return drift, err
	}
	if err := tx.Commit(); err != nil {
		return GrantDrift{}, errors.New("unable to finish scoped grant inspection")
	}
	if len(drift.Missing) != 0 || len(drift.Unexpected) != 0 {
		return drift, errors.New("scoped runtime grant drift detected")
	}
	return drift, nil
}

func (c GrantContract) checkTx(ctx context.Context, tx *sql.Tx) (GrantDrift, error) {
	// Routine argument formatting must be independent of the caller's pool
	// search path, especially when the signature references an extension type.
	if _, err := tx.ExecContext(ctx, "SET LOCAL search_path = pg_catalog"); err != nil {
		return GrantDrift{}, errors.New("unable to normalize grant inspection context")
	}
	var session, current string
	var exists bool
	err := tx.QueryRowContext(ctx, `
SELECT session_user, current_user,
       EXISTS (SELECT 1 FROM pg_catalog.pg_namespace
               WHERE nspname = $1 AND nspname <> 'information_schema'
                 AND pg_catalog.left(nspname, 3) <> 'pg_')
`, c.Schema).Scan(&session, &current, &exists)
	if err != nil || !exists || session != c.Role || current != c.Role {
		return GrantDrift{}, errors.New("scoped grant inspection identity or schema mismatch")
	}

	const query = `
-- Keep the UNION object type text, not name (63-byte identifier limit).
WITH target AS (
    SELECT oid, nspname FROM pg_catalog.pg_namespace WHERE nspname = $1
), privileges AS (
    SELECT 'schema' AS kind, n.nspname::text AS object, p.name AS privilege
    FROM target n CROSS JOIN (VALUES ('USAGE'), ('CREATE')) p(name)
    WHERE pg_catalog.has_schema_privilege(current_user, n.oid, p.name)
    UNION ALL
    SELECT 'schema', n.nspname::text, p.name || ' WITH GRANT OPTION'
    FROM target n CROSS JOIN (VALUES ('USAGE'), ('CREATE')) p(name)
    WHERE pg_catalog.has_schema_privilege(current_user, n.oid, p.name || ' WITH GRANT OPTION')
    UNION ALL
    SELECT 'relation', c.relname::text, p.name || o.suffix
    FROM pg_catalog.pg_class c JOIN target n ON n.oid = c.relnamespace
    CROSS JOIN (VALUES ('SELECT'), ('INSERT'), ('UPDATE'), ('DELETE'),
                       ('TRUNCATE'), ('REFERENCES'), ('TRIGGER'), ('MAINTAIN')) p(name)
    CROSS JOIN (VALUES (''), (' WITH GRANT OPTION')) o(suffix)
    WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
      AND pg_catalog.has_table_privilege(current_user, c.oid, p.name || o.suffix)
    UNION ALL
    SELECT 'sequence', c.relname::text, p.name || o.suffix
    FROM pg_catalog.pg_class c JOIN target n ON n.oid = c.relnamespace
    CROSS JOIN (VALUES ('USAGE'), ('SELECT'), ('UPDATE')) p(name)
    CROSS JOIN (VALUES (''), (' WITH GRANT OPTION')) o(suffix)
    WHERE c.relkind = 'S'
      AND pg_catalog.has_sequence_privilege(current_user, c.oid, p.name || o.suffix)
    UNION ALL
    SELECT 'column', pg_catalog.quote_ident(c.relname) || '.' || pg_catalog.quote_ident(a.attname),
           p.name || o.suffix
    FROM pg_catalog.pg_class c JOIN target n ON n.oid = c.relnamespace
    JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
    CROSS JOIN (VALUES ('SELECT'), ('INSERT'), ('UPDATE'), ('REFERENCES')) p(name)
    CROSS JOIN (VALUES (''), (' WITH GRANT OPTION')) o(suffix)
    WHERE $2::boolean AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
      AND a.attnum > 0 AND NOT a.attisdropped
      AND pg_catalog.has_column_privilege(current_user, c.oid, a.attnum, p.name || o.suffix)
    UNION ALL
    SELECT 'routine', pg_catalog.quote_ident(f.proname) || '(' ||
           pg_catalog.pg_get_function_identity_arguments(f.oid) || ')',
           'EXECUTE' || o.suffix
    FROM pg_catalog.pg_proc f JOIN target n ON n.oid = f.pronamespace
    CROSS JOIN (VALUES (''), (' WITH GRANT OPTION')) o(suffix)
    WHERE $2::boolean
      AND pg_catalog.has_function_privilege(current_user, f.oid, 'EXECUTE' || o.suffix)
)
SELECT kind, object, privilege FROM privileges ORDER BY kind, object, privilege
`
	rows, err := tx.QueryContext(ctx, query, c.Schema, c.Extended)
	if err != nil {
		return GrantDrift{}, errors.New("unable to inspect scoped grants")
	}
	defer rows.Close()

	var actual []Grant
	for rows.Next() {
		var g Grant
		if err := rows.Scan(&g.Kind, &g.Object, &g.Privilege); err != nil {
			return GrantDrift{}, errors.New("unable to read scoped grant")
		}
		actual = append(actual, g)
	}
	if err := rows.Err(); err != nil {
		return GrantDrift{}, errors.New("incomplete scoped grant inspection")
	}
	return CompareGrants(c.Grants, actual), nil
}
