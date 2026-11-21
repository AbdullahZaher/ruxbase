package e2e_test

import (
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestVersionedMigrationPlan_ExecutionAndRevert(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	plan := &core.MigrationPlan{
		Version:     "20260330100000_create_customers",
		Description: "Create customers table with contact details",
		UpSQL: `
			CREATE TABLE customers (
				id text PRIMARY KEY,
				name text NOT NULL,
				email text UNIQUE NOT NULL,
				credit_limit numeric(10, 2) DEFAULT 1000.00
			);
		`,
		DownSQL: `DROP TABLE IF EXISTS customers;`,
	}

	// 1. Checksum calculation
	expectedHash := plan.CalculateChecksum()
	if expectedHash == "" {
		t.Fatalf("Expected non-empty checksum from CalculateChecksum")
	}

	// 2. Execute plan
	if err := app.ExecuteMigrationPlan(plan); err != nil {
		t.Fatalf("ExecuteMigrationPlan failed: %v", err)
	}

	// Verify table was created
	if !app.HasTable("customers") {
		t.Fatalf("Table customers was not created by migration")
	}

	// 3. Negative check: re-executing already applied migration must fail
	err = app.ExecuteMigrationPlan(plan)
	if err == nil {
		t.Fatalf("Expected re-executing already applied migration to fail, but succeeded")
	}
	if !strings.Contains(err.Error(), "already applied") {
		t.Errorf("Expected 'already applied' error message, got: %v", err)
	}

	// 4. Negative check: executing plan with tampered checksum must fail
	tamperedPlan := &core.MigrationPlan{
		Version:  "20260330100001_tampered",
		Checksum: "bad_checksum_12345",
		UpSQL:    "CREATE TABLE tampered (id text);",
	}
	err = app.ExecuteMigrationPlan(tamperedPlan)
	if err == nil {
		t.Fatalf("Expected execution with mismatched checksum to fail, but succeeded")
	}

	// 5. Revert migration
	if err := app.RevertMigrationPlan(plan); err != nil {
		t.Fatalf("RevertMigrationPlan failed: %v", err)
	}

	// Verify table was dropped
	if app.HasTable("customers") {
		t.Fatalf("Table customers should have been dropped by revert")
	}
}

func TestMigrationIntegrity_TamperDetection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	planA := &core.MigrationPlan{
		Version:     "20260330110000_init_billing",
		Description: "Initialize billing accounts",
		UpSQL:       "CREATE TABLE billing_accounts (id text PRIMARY KEY, balance numeric(12, 2) DEFAULT 0.00);",
		DownSQL:     "DROP TABLE IF EXISTS billing_accounts;",
	}
	planB := &core.MigrationPlan{
		Version:     "20260330110001_init_invoices",
		Description: "Initialize invoices",
		UpSQL:       "CREATE TABLE invoices (id text PRIMARY KEY, amount numeric(10, 2) NOT NULL);",
		DownSQL:     "DROP TABLE IF EXISTS invoices;",
	}

	if err := app.ExecuteMigrationPlan(planA); err != nil {
		t.Fatalf("Failed to execute planA: %v", err)
	}
	if err := app.ExecuteMigrationPlan(planB); err != nil {
		t.Fatalf("Failed to execute planB: %v", err)
	}

	// 1. Verify integrity against matching codebase plans -> 0 warnings
	warnings, err := app.VerifyMigrationIntegrity([]*core.MigrationPlan{planA, planB})
	if err != nil {
		t.Fatalf("VerifyMigrationIntegrity failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("Expected 0 integrity warnings for untampered migrations, got %d", len(warnings))
	}

	// 2. Tamper with planB in code (alter UpSQL)
	tamperedPlanB := &core.MigrationPlan{
		Version:     planB.Version,
		Description: planB.Description,
		UpSQL:       "CREATE TABLE invoices (id text PRIMARY KEY, amount numeric(10, 2) NOT NULL, extra text);",
		DownSQL:     planB.DownSQL,
	}

	warnings, err = app.VerifyMigrationIntegrity([]*core.MigrationPlan{planA, tamperedPlanB})
	if err != nil {
		t.Fatalf("VerifyMigrationIntegrity failed: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("Expected 1 tamper warning for modified planB, got %d", len(warnings))
	}
	if !strings.Contains(warnings[0].Message, "mismatch") {
		t.Errorf("Expected checksum mismatch message, got: %s", warnings[0].Message)
	}
}

func TestSchemaDrift_DetectionAndClassification(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a Collection in Ruxbase
	col := core.NewBaseCollection("drift_articles")
	col.Fields.Add(&core.TextField{Name: "title", Required: true})
	col.Fields.Add(&core.TextField{Name: "content"})
	if err := app.Save(col); err != nil {
		t.Fatalf("Failed to save drift_articles collection: %v", err)
	}

	// Initial drift check on clean state
	report, err := app.DetectSchemaDrift("public")
	if err != nil {
		t.Fatalf("DetectSchemaDrift failed: %v", err)
	}

	// Verify no drift for drift_articles
	for _, diff := range report.Differences {
		if diff.Table == "drift_articles" {
			t.Errorf("Unexpected drift on newly created collection: %v", diff)
		}
	}

	// 2. Introduce drift via direct DDL: Drop column "content"
	if _, err := app.DB().NewQuery("ALTER TABLE drift_articles DROP COLUMN content").Execute(); err != nil {
		t.Fatalf("Failed to drop column content: %v", err)
	}

	// 3. Introduce drift via direct DDL: Add extra column "rogue_column"
	if _, err := app.DB().NewQuery("ALTER TABLE drift_articles ADD COLUMN rogue_column integer DEFAULT 0").Execute(); err != nil {
		t.Fatalf("Failed to add rogue_column: %v", err)
	}

	// 4. Introduce untracked table
	if _, err := app.DB().NewQuery("CREATE TABLE unmanaged_shadow_table (id text PRIMARY KEY, secret text)").Execute(); err != nil {
		t.Fatalf("Failed to create unmanaged_shadow_table: %v", err)
	}

	// 5. Run DetectSchemaDrift and assert detected discrepancies
	driftReport, err := app.DetectSchemaDrift("public")
	if err != nil {
		t.Fatalf("DetectSchemaDrift failed: %v", err)
	}

	if !driftReport.HasDrift {
		t.Fatalf("Expected HasDrift to be true")
	}

	categories := make(map[string]core.SchemaDriftDifference)
	for _, diff := range driftReport.Differences {
		key := diff.Category + ":" + diff.Table
		if diff.Column != "" {
			key += ":" + diff.Column
		}
		categories[key] = diff
	}

	// Assert COLUMN_MISSING for content
	if _, ok := categories["COLUMN_MISSING:drift_articles:content"]; !ok {
		t.Errorf("Expected COLUMN_MISSING for drift_articles:content, found: %v", driftReport.Differences)
	}

	// Assert COLUMN_EXTRA for rogue_column
	if _, ok := categories["COLUMN_EXTRA:drift_articles:rogue_column"]; !ok {
		t.Errorf("Expected COLUMN_EXTRA for drift_articles:rogue_column, found: %v", driftReport.Differences)
	}

	// Assert TABLE_UNTRACKED for unmanaged_shadow_table
	if _, ok := categories["TABLE_UNTRACKED:unmanaged_shadow_table"]; !ok {
		t.Errorf("Expected TABLE_UNTRACKED for unmanaged_shadow_table, found: %v", driftReport.Differences)
	}
}
