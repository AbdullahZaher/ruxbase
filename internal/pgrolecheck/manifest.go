package pgrolecheck

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
)

// GrantManifest declares the full non-system schema inventory of one database
// and the effective grants expected within each schema. It does not imply a
// complete authority model: role administration, default ACLs, RLS and other
// database-level object classes are outside this manifest.
type GrantManifest struct {
	Role    string          `json:"role"`
	Schemas []GrantContract `json:"schemas"`
}

type SchemaGrantDrift struct {
	Schema string     `json:"schema"`
	Drift  GrantDrift `json:"drift"`
}

type ManifestDrift struct {
	MissingSchemas    []string           `json:"missingSchemas"`
	UnexpectedSchemas []string           `json:"unexpectedSchemas"`
	Grants            []SchemaGrantDrift `json:"grants"`
}

func (m GrantManifest) validate() error {
	if m.Role == "" || len(m.Schemas) == 0 {
		return errors.New("explicit manifest identity and schemas required")
	}
	seen := map[string]bool{}
	for _, schema := range m.Schemas {
		if schema.Role != m.Role || schema.Schema == "" || schema.Schema == "information_schema" ||
			strings.HasPrefix(schema.Schema, "pg_") || !schema.Extended || schema.Grants == nil ||
			seen[schema.Schema] {
			return errors.New("invalid or duplicate extended schema contract")
		}
		seen[schema.Schema] = true
	}
	return nil
}

// CompareSchemaInventory rejects omitted and unknown schemas, even if no
// effective grants exist on those schemas.
func CompareSchemaInventory(expected, actual []string) (missing, unexpected []string) {
	want := map[string]bool{}
	got := map[string]bool{}
	for _, name := range expected {
		want[name] = true
	}
	for _, name := range actual {
		got[name] = true
	}
	missing, unexpected = []string{}, []string{}
	for name := range want {
		if !got[name] {
			missing = append(missing, name)
		}
	}
	for name := range got {
		if !want[name] {
			unexpected = append(unexpected, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	return missing, unexpected
}

// Check compares schema inventory and all extended effective grant contracts
// using one read-only repeatable-read transaction. No grants are modified.
func (m GrantManifest) Check(ctx context.Context, db *sql.DB) (ManifestDrift, error) {
	if err := m.validate(); err != nil {
		return ManifestDrift{}, err
	}
	if db == nil {
		return ManifestDrift{}, errors.New("missing manifest connection")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ManifestDrift{}, errors.New("unable to begin grant manifest inspection")
	}
	defer func() { _ = tx.Rollback() }()
	var session, current string
	if err := tx.QueryRowContext(ctx, "SELECT session_user, current_user").Scan(&session, &current); err != nil ||
		session != m.Role || current != m.Role {
		return ManifestDrift{}, errors.New("manifest connection identity mismatch")
	}
	rows, err := tx.QueryContext(ctx, `
SELECT nspname FROM pg_catalog.pg_namespace
WHERE nspname <> 'information_schema'
  AND pg_catalog.left(nspname, 3) <> 'pg_'
ORDER BY nspname
`)
	if err != nil {
		return ManifestDrift{}, errors.New("unable to inspect schema inventory")
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return ManifestDrift{}, errors.New("unable to read schema inventory")
		}
		actual = append(actual, name)
	}
	if err := rows.Err(); err != nil {
		return ManifestDrift{}, errors.New("incomplete schema inventory")
	}
	expected := make([]string, 0, len(m.Schemas))
	for _, schema := range m.Schemas {
		expected = append(expected, schema.Schema)
	}
	missing, unexpected := CompareSchemaInventory(expected, actual)
	result := ManifestDrift{MissingSchemas: missing, UnexpectedSchemas: unexpected, Grants: []SchemaGrantDrift{}}
	if len(missing) != 0 || len(unexpected) != 0 {
		return result, errors.New("schema inventory drift detected")
	}
	for _, schema := range m.Schemas {
		drift, err := schema.checkTx(ctx, tx)
		if err != nil {
			return result, err
		}
		if len(drift.Missing) != 0 || len(drift.Unexpected) != 0 {
			result.Grants = append(result.Grants, SchemaGrantDrift{Schema: schema.Schema, Drift: drift})
		}
	}
	sort.Slice(result.Grants, func(i, j int) bool { return result.Grants[i].Schema < result.Grants[j].Schema })
	if err := tx.Commit(); err != nil {
		return ManifestDrift{}, errors.New("unable to finish manifest inspection")
	}
	if len(result.Grants) != 0 {
		return result, errors.New("grant manifest drift detected")
	}
	return result, nil
}
