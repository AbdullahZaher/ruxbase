package core

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/core/cluster"
	"github.com/pocketbase/dbx"
)

// MigrationPlan represents a typed, versioned database migration with immutable checksums.
type MigrationPlan struct {
	Version     string `json:"version"` // Unique version identifier (e.g. "20260330120000_add_payments")
	Description string `json:"description"`
	Checksum    string `json:"checksum"` // SHA-256 fingerprint
	UpSQL       string `json:"upSQL"`
	DownSQL     string `json:"downSQL"`
	AppliedAt   int64  `json:"appliedAt,omitempty"`
}

// CalculateChecksum computes the SHA-256 hash of a migration plan.
func (p *MigrationPlan) CalculateChecksum() string {
	hasher := sha256.New()
	hasher.Write([]byte(p.Version))
	hasher.Write([]byte("\n--UP--\n"))
	hasher.Write([]byte(p.UpSQL))
	hasher.Write([]byte("\n--DOWN--\n"))
	hasher.Write([]byte(p.DownSQL))
	return hex.EncodeToString(hasher.Sum(nil))
}

// SchemaDriftDifference records a single discrepancy between metadata and physical PostgreSQL catalog.
type SchemaDriftDifference struct {
	Category    string `json:"category"` // "TABLE_MISSING", "TABLE_UNTRACKED", "COLUMN_MISSING", "COLUMN_EXTRA", "TYPE_MISMATCH", "INDEX_MISSING"
	Table       string `json:"table"`
	Column      string `json:"column,omitempty"`
	Expected    string `json:"expected,omitempty"`
	Actual      string `json:"actual,omitempty"`
	Description string `json:"description"`
}

// SchemaDriftReport aggregates all discovered drift differences.
type SchemaDriftReport struct {
	HasDrift    bool                    `json:"hasDrift"`
	CheckedAt   time.Time               `json:"checkedAt"`
	Differences []SchemaDriftDifference `json:"differences"`
}

// MigrationIntegrityWarning records a tamper or mismatch detection in migration ledger.
type MigrationIntegrityWarning struct {
	File             string `json:"file"`
	RecordedChecksum string `json:"recordedChecksum"`
	ActualChecksum   string `json:"actualChecksum"`
	Message          string `json:"message"`
}

// ExecuteMigrationPlan executes a versioned migration plan inside an advisory-locked transaction
// and records its immutable checksum in the migrations ledger.
func (app *BaseApp) ExecuteMigrationPlan(plan *MigrationPlan) error {
	if plan == nil || plan.Version == "" {
		return errors.New("cannot execute nil or empty migration plan")
	}

	computedHash := plan.CalculateChecksum()
	if plan.Checksum == "" {
		plan.Checksum = computedHash
	} else if plan.Checksum != computedHash {
		return fmt.Errorf("migration plan %q checksum mismatch: expected %q, got %q", plan.Version, plan.Checksum, computedHash)
	}

	return app.RunInTransaction(func(txApp App) error {
		// Cluster lock to ensure single-instance migration execution
		if err := cluster.XactLock(txApp.DB(), "migrations:"+DefaultMigrationsTable); err != nil {
			return err
		}

		// Ensure checksum column exists in _migrations
		_, err := txApp.DB().NewQuery(
			"ALTER TABLE {{_migrations}} ADD COLUMN IF NOT EXISTS [[checksum]] text DEFAULT ''",
		).Execute()
		if err != nil {
			return fmt.Errorf("failed to ensure checksum column in _migrations: %w", err)
		}

		// Check if already applied
		var exists int
		err = txApp.DB().Select("(1)").
			From(DefaultMigrationsTable).
			Where(dbx.HashExp{"file": plan.Version}).
			Limit(1).
			Row(&exists)
		if err == nil && exists > 0 {
			return fmt.Errorf("migration %q is already applied", plan.Version)
		}

		// Execute Up DDL
		if strings.TrimSpace(plan.UpSQL) != "" {
			if _, err := txApp.DB().NewQuery(plan.UpSQL).Execute(); err != nil {
				return fmt.Errorf("failed to execute migration up SQL for %q: %w", plan.Version, err)
			}
		}

		// Record in ledger
		now := time.Now().UnixMicro()
		plan.AppliedAt = now
		_, err = txApp.DB().Insert(DefaultMigrationsTable, dbx.Params{
			"file":     plan.Version,
			"applied":  now,
			"checksum": plan.Checksum,
		}).Execute()
		if err != nil {
			return fmt.Errorf("failed to record applied migration %q: %w", plan.Version, err)
		}

		return nil
	})
}

// RevertMigrationPlan reverses a previously applied migration plan using its DownSQL.
func (app *BaseApp) RevertMigrationPlan(plan *MigrationPlan) error {
	if plan == nil || plan.Version == "" {
		return errors.New("cannot revert nil or empty migration plan")
	}

	return app.RunInTransaction(func(txApp App) error {
		if err := cluster.XactLock(txApp.DB(), "migrations:"+DefaultMigrationsTable); err != nil {
			return err
		}

		// Check if applied
		var exists int
		err := txApp.DB().Select("(1)").
			From(DefaultMigrationsTable).
			Where(dbx.HashExp{"file": plan.Version}).
			Limit(1).
			Row(&exists)
		if err != nil || exists == 0 {
			return fmt.Errorf("migration %q is not applied, cannot revert", plan.Version)
		}

		// Execute Down DDL
		if strings.TrimSpace(plan.DownSQL) != "" {
			if _, err := txApp.DB().NewQuery(plan.DownSQL).Execute(); err != nil {
				return fmt.Errorf("failed to execute migration down SQL for %q: %w", plan.Version, err)
			}
		}

		// Remove from ledger
		_, err = txApp.DB().Delete(DefaultMigrationsTable, dbx.HashExp{"file": plan.Version}).Execute()
		if err != nil {
			return fmt.Errorf("failed to delete migration record for %q: %w", plan.Version, err)
		}

		return nil
	})
}

// VerifyMigrationIntegrity verifies that all applied migrations in the database
// match the expected checksums of the provided migration plans.
func (app *BaseApp) VerifyMigrationIntegrity(plans []*MigrationPlan) ([]*MigrationIntegrityWarning, error) {
	type migrationRow struct {
		File     string `db:"file"`
		Checksum string `db:"checksum"`
	}

	// Ensure checksum column exists
	_, _ = app.DB().NewQuery(
		"ALTER TABLE {{_migrations}} ADD COLUMN IF NOT EXISTS [[checksum]] text DEFAULT ''",
	).Execute()

	rows := []migrationRow{}
	err := app.ConcurrentDB().Select("file", "COALESCE(checksum, '') AS checksum").
		From(DefaultMigrationsTable).
		All(&rows)
	if err != nil {
		return nil, fmt.Errorf("failed to query migrations table: %w", err)
	}

	rowByFile := make(map[string]migrationRow, len(rows))
	for _, r := range rows {
		rowByFile[r.File] = r
	}

	warnings := []*MigrationIntegrityWarning{}
	for _, plan := range plans {
		r, applied := rowByFile[plan.Version]
		if !applied {
			continue // not applied yet
		}

		expectedHash := plan.CalculateChecksum()
		if r.Checksum != "" && r.Checksum != expectedHash {
			warnings = append(warnings, &MigrationIntegrityWarning{
				File:             plan.Version,
				RecordedChecksum: r.Checksum,
				ActualChecksum:   expectedHash,
				Message: fmt.Sprintf(
					"Migration %q checksum mismatch: database has %q but codebase computes %q (possible tampering)",
					plan.Version, r.Checksum, expectedHash,
				),
			})
		}
	}

	return warnings, nil
}

// DetectSchemaDrift compares the registered Collections in Ruxbase against the
// actual PostgreSQL catalog for the specified schema.
func (app *BaseApp) DetectSchemaDrift(schema string) (*SchemaDriftReport, error) {
	if schema == "" {
		schema = "public"
	}

	report := &SchemaDriftReport{
		CheckedAt:   time.Now(),
		Differences: []SchemaDriftDifference{},
	}

	// 1. Get all collections
	collections, err := app.FindAllCollections(CollectionTypeBase, CollectionTypeAuth)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch collections: %w", err)
	}

	// 2. Get physical tables in PostgreSQL schema
	physicalTables, err := app.IntrospectSchemaTables(schema)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect physical tables: %w", err)
	}

	physicalTableMap := make(map[string]*NativeTable, len(physicalTables))
	for _, tbl := range physicalTables {
		physicalTableMap[strings.ToLower(tbl.Name)] = tbl
	}

	collectionMap := make(map[string]*Collection, len(collections))
	for _, col := range collections {
		collectionMap[strings.ToLower(col.Name)] = col
	}

	// 3. Check for missing tables (collection exists, physical table does not)
	for _, col := range collectionMap {
		if !app.HasTable(col.Name) {
			report.Differences = append(report.Differences, SchemaDriftDifference{
				Category:    "TABLE_MISSING",
				Table:       col.Name,
				Description: fmt.Sprintf("Collection %q is defined in metadata but table does not exist in PostgreSQL", col.Name),
			})
			continue
		}

		tbl, err := app.IntrospectTable(col.Name)
		if err != nil {
			report.Differences = append(report.Differences, SchemaDriftDifference{
				Category:    "TABLE_MISSING",
				Table:       col.Name,
				Description: fmt.Sprintf("Failed to introspect table %q: %v", col.Name, err),
			})
			continue
		}

		colMap := make(map[string]*NativeColumn, len(tbl.Columns))
		for _, c := range tbl.Columns {
			colMap[strings.ToLower(c.Name)] = c
		}

		// Check fields in collection against columns in table
		for _, field := range col.Fields {
			fNameLower := strings.ToLower(field.GetName())
			pgCol, exists := colMap[fNameLower]
			if !exists {
				// Search fields may be generated columns or view-backed
				if field.Type() == FieldTypeSearch {
					continue
				}
				report.Differences = append(report.Differences, SchemaDriftDifference{
					Category:    "COLUMN_MISSING",
					Table:       col.Name,
					Column:      field.GetName(),
					Expected:    field.Type(),
					Description: fmt.Sprintf("Field %q (type: %s) in collection %q is missing from PostgreSQL table", field.GetName(), field.Type(), col.Name),
				})
				continue
			}

			// Validate type compatibility
			expectedCol, err := MapFieldToNativeColumn(field)
			if err == nil && expectedCol != nil {
				if !areDataTypesCompatible(expectedCol.DataType, pgCol.DataType, pgCol.UDTName) {
					report.Differences = append(report.Differences, SchemaDriftDifference{
						Category:    "TYPE_MISMATCH",
						Table:       col.Name,
						Column:      field.GetName(),
						Expected:    expectedCol.DataType,
						Actual:      pgCol.DataType,
						Description: fmt.Sprintf("Column %q in table %q has physical type %q, expected %q", field.GetName(), col.Name, pgCol.DataType, expectedCol.DataType),
					})
				}
			}
		}

		// Check for extra columns in PostgreSQL not present in collection
		fieldMap := make(map[string]Field, len(col.Fields))
		for _, f := range col.Fields {
			fieldMap[strings.ToLower(f.GetName())] = f
		}
		for _, pgCol := range tbl.Columns {
			cNameLower := strings.ToLower(pgCol.Name)
			if _, exists := fieldMap[cNameLower]; !exists {
				report.Differences = append(report.Differences, SchemaDriftDifference{
					Category:    "COLUMN_EXTRA",
					Table:       col.Name,
					Column:      pgCol.Name,
					Actual:      pgCol.DataType,
					Description: fmt.Sprintf("PostgreSQL table %q has extra column %q (type: %s) not tracked in Collection", col.Name, pgCol.Name, pgCol.DataType),
				})
			}
		}
	}

	// 4. Check for untracked user tables in schema
	for _, tbl := range physicalTables {
		tNameLower := strings.ToLower(tbl.Name)
		// Ignore internal tables
		if isInternalTable(tbl.Name) {
			continue
		}
		if _, exists := collectionMap[tNameLower]; !exists {
			report.Differences = append(report.Differences, SchemaDriftDifference{
				Category:    "TABLE_UNTRACKED",
				Table:       tbl.Name,
				Description: fmt.Sprintf("PostgreSQL table %q exists in schema %q but is not exposed or tracked as a Collection", tbl.Name, schema),
			})
		}
	}

	report.HasDrift = len(report.Differences) > 0
	return report, nil
}

func isInternalTable(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "_") ||
		strings.HasPrefix(lower, "pg_") ||
		strings.HasPrefix(lower, "sqlite_") ||
		strings.HasPrefix(lower, "information_schema")
}

func areDataTypesCompatible(expected, actual, udt string) bool {
	exp := strings.ToLower(strings.TrimSpace(expected))
	act := strings.ToLower(strings.TrimSpace(actual))
	u := strings.ToLower(strings.TrimSpace(udt))

	if exp == act || strings.Contains(act, exp) || strings.Contains(exp, act) {
		return true
	}

	// Numeric compatibility
	if strings.HasPrefix(exp, "numeric") && (strings.HasPrefix(act, "numeric") || u == "numeric") {
		return true
	}
	if exp == "double precision" && (act == "double precision" || u == "float8") {
		return true
	}
	if exp == "bigint" && (act == "bigint" || u == "int8" || u == "integer" || act == "integer") {
		return true
	}

	// Text compatibility
	if (exp == "text" || strings.HasPrefix(exp, "varchar")) &&
		(act == "text" || strings.HasPrefix(act, "varchar") || strings.HasPrefix(act, "character varying")) {
		return true
	}

	// Timestamp compatibility
	if strings.Contains(exp, "timestamp") && (strings.Contains(act, "timestamp") || u == "timestamptz") {
		return true
	}

	// JSON compatibility
	if (exp == "jsonb" || exp == "json") && (act == "jsonb" || act == "json" || u == "jsonb" || u == "json") {
		return true
	}

	// Bool compatibility
	if (exp == "boolean" || exp == "bool") && (act == "boolean" || act == "bool" || u == "bool") {
		return true
	}

	return false
}
