package e2e_test

import (
	"math"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

func TestExactNumeric_IntrospectionAndValidation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a native table with NUMERIC(10, 2) and NUMERIC(14, 4)
	setupSQL := `
		CREATE TABLE financial_transactions (
			id text PRIMARY KEY,
			description text NOT NULL,
			amount numeric(10, 2) NOT NULL,
			exchange_rate numeric(14, 4) DEFAULT 1.0000,
			fee numeric(6, 2) DEFAULT 0.00
		);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create financial_transactions table: %v", err)
	}

	// 2. Introspect table and verify precision/scale
	tbl, err := app.IntrospectTable("financial_transactions")
	if err != nil {
		t.Fatalf("IntrospectTable failed: %v", err)
	}

	amountCol := tbl.FindColumn("amount")
	if amountCol == nil {
		t.Fatalf("Column amount not found in catalog")
	}
	if amountCol.NumericPrecision == nil || *amountCol.NumericPrecision != 10 {
		t.Errorf("Expected amount precision 10, got %v", amountCol.NumericPrecision)
	}
	if amountCol.NumericScale == nil || *amountCol.NumericScale != 2 {
		t.Errorf("Expected amount scale 2, got %v", amountCol.NumericScale)
	}

	rateCol := tbl.FindColumn("exchange_rate")
	if rateCol == nil {
		t.Fatalf("Column exchange_rate not found in catalog")
	}
	if rateCol.NumericPrecision == nil || *rateCol.NumericPrecision != 14 {
		t.Errorf("Expected exchange_rate precision 14, got %v", rateCol.NumericPrecision)
	}
	if rateCol.NumericScale == nil || *rateCol.NumericScale != 4 {
		t.Errorf("Expected exchange_rate scale 4, got %v", rateCol.NumericScale)
	}

	// 3. Expose table as Collection
	listRule := ""
	col, err := app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "financial_transactions",
		CollectionName: "financial_transactions",
		ListRule:       &listRule,
	})
	if err != nil {
		t.Fatalf("ExposeNativeTable failed: %v", err)
	}

	// Verify NumberField configuration
	amountField, _ := col.Fields.GetByName("amount").(*core.NumberField)
	if amountField == nil {
		t.Fatalf("Field amount is not a NumberField")
	}
	if amountField.Precision == nil || *amountField.Precision != 10 {
		t.Errorf("Expected amountField Precision 10, got %v", amountField.Precision)
	}
	if amountField.Scale == nil || *amountField.Scale != 2 {
		t.Errorf("Expected amountField Scale 2, got %v", amountField.Scale)
	}

	// 4. Insert valid record with exact numeric values
	rec := core.NewRecord(col)
	rec.Set("id", "txn000000000001")
	rec.Set("description", "International Wire")
	rec.Set("amount", 12345.67)
	rec.Set("exchange_rate", 1.2345)
	rec.Set("fee", 15.50)
	if err := app.Save(rec); err != nil {
		t.Fatalf("Failed to save valid financial transaction: %v", err)
	}

	// Verify persistence in PostgreSQL
	fetched, err := app.FindRecordById("financial_transactions", "txn000000000001")
	if err != nil {
		t.Fatalf("Failed to fetch saved record: %v", err)
	}
	if math.Abs(fetched.GetFloat("amount")-12345.67) > 1e-4 {
		t.Errorf("Expected amount 12345.67, got %v", fetched.GetFloat("amount"))
	}
	if math.Abs(fetched.GetFloat("exchange_rate")-1.2345) > 1e-4 {
		t.Errorf("Expected exchange_rate 1.2345, got %v", fetched.GetFloat("exchange_rate"))
	}

	// 5. Negative check: scale violation (amount has 3 decimal places, max scale is 2)
	invalidScaleRec := core.NewRecord(col)
	invalidScaleRec.Set("id", "txn000000000002")
	invalidScaleRec.Set("description", "Scale Overflow")
	invalidScaleRec.Set("amount", 99.999) // scale 3 > 2
	if err := app.Save(invalidScaleRec); err == nil {
		t.Fatalf("Expected Save with scale overflow (99.999 on scale 2) to fail, but succeeded")
	}

	// 6. Negative check: precision violation (amount has 11 digits, max precision is 10)
	invalidPrecRec := core.NewRecord(col)
	invalidPrecRec.Set("id", "txn000000000003")
	invalidPrecRec.Set("description", "Precision Overflow")
	invalidPrecRec.Set("amount", 12345678901.00) // 11 digits > 10
	if err := app.Save(invalidPrecRec); err == nil {
		t.Fatalf("Expected Save with precision overflow to fail, but succeeded")
	}
}

func TestNativeTimestamps_RoundtripAndUTC(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create table with TIMESTAMPTZ column
	setupSQL := `
		CREATE TABLE audit_events (
			id text PRIMARY KEY,
			action text NOT NULL,
			event_time timestamptz NOT NULL,
			resolved_at timestamptz
		);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create audit_events table: %v", err)
	}

	// 2. Expose table as Collection
	listRule := ""
	col, err := app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "audit_events",
		CollectionName: "audit_events",
		ListRule:       &listRule,
	})
	if err != nil {
		t.Fatalf("ExposeNativeTable failed: %v", err)
	}

	// Verify FieldTypeDate mapping
	eventTimeField, ok := col.Fields.GetByName("event_time").(*core.DateField)
	if !ok || eventTimeField == nil {
		t.Fatalf("Expected event_time to be mapped to DateField")
	}

	// 3. Save record with specific UTC time
	nowUTC := time.Date(2026, 3, 30, 14, 30, 0, 0, time.UTC)
	dt, err := types.ParseDateTime(nowUTC)
	if err != nil {
		t.Fatalf("Failed to parse datetime: %v", err)
	}

	rec := core.NewRecord(col)
	rec.Set("id", "audit0000000001")
	rec.Set("action", "USER_LOGIN")
	rec.Set("event_time", dt)
	if err := app.Save(rec); err != nil {
		t.Fatalf("Failed to save audit record with native timestamp: %v", err)
	}

	// 4. Retrieve and verify roundtrip fidelity
	fetched, err := app.FindRecordById("audit_events", "audit0000000001")
	if err != nil {
		t.Fatalf("Failed to fetch audit record: %v", err)
	}

	retrievedTime := fetched.GetDateTime("event_time").Time()
	if !retrievedTime.Equal(nowUTC) {
		t.Errorf("Timestamp roundtrip mismatch: expected %v, got %v", nowUTC, retrievedTime)
	}

	// Nullable timestamp should remain zero
	resolvedTime := fetched.GetDateTime("resolved_at")
	if !resolvedTime.IsZero() {
		t.Errorf("Expected zero resolved_at, got %v", resolvedTime)
	}
}

func TestGeneratedColumns_CatalogIntrospectionAndComputation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create native table with STORED generated columns
	setupSQL := `
		CREATE TABLE order_line_items (
			id text PRIMARY KEY,
			product_name text NOT NULL,
			unit_price numeric(10, 2) NOT NULL,
			quantity integer NOT NULL,
			total_price numeric(12, 2) GENERATED ALWAYS AS (unit_price * quantity) STORED,
			summary text GENERATED ALWAYS AS (product_name || ' x ' || quantity::text) STORED
		);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create order_line_items table: %v", err)
	}

	// 2. Introspect table and verify generated column attributes
	tbl, err := app.IntrospectTable("order_line_items")
	if err != nil {
		t.Fatalf("IntrospectTable failed: %v", err)
	}

	totalCol := tbl.FindColumn("total_price")
	if totalCol == nil {
		t.Fatalf("Column total_price not found")
	}
	if !totalCol.Generated() {
		t.Errorf("Expected total_price to be flagged as generated, got is_generated=%q", totalCol.IsGenerated)
	}

	summaryCol := tbl.FindColumn("summary")
	if summaryCol == nil {
		t.Fatalf("Column summary not found")
	}
	if !summaryCol.Generated() {
		t.Errorf("Expected summary to be flagged as generated, got is_generated=%q", summaryCol.IsGenerated)
	}

	// 3. Expose table as Collection
	listRule := ""
	col, err := app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "order_line_items",
		CollectionName: "order_line_items",
		ListRule:       &listRule,
	})
	if err != nil {
		t.Fatalf("ExposeNativeTable failed: %v", err)
	}

	totalField, _ := col.Fields.GetByName("total_price").(*core.NumberField)
	if totalField == nil || !totalField.IsGenerated() {
		t.Fatalf("Expected total_price field to be generated NumberField")
	}

	summaryField, _ := col.Fields.GetByName("summary").(*core.TextField)
	if summaryField == nil || !summaryField.IsGenerated() {
		t.Fatalf("Expected summary field to be generated TextField")
	}

	// 4. Insert record WITHOUT supplying generated columns
	// dbExport must omit total_price and summary so PostgreSQL computes them without SQLSTATE 42601
	rec := core.NewRecord(col)
	rec.Set("id", "item00000000001")
	rec.Set("product_name", "Mechanical Keyboard")
	rec.Set("unit_price", 129.50)
	rec.Set("quantity", 3)
	if err := app.Save(rec); err != nil {
		t.Fatalf("Failed to save order line item with generated columns: %v", err)
	}

	// 5. Verify PostgreSQL computed the values accurately
	fetched, err := app.FindRecordById("order_line_items", "item00000000001")
	if err != nil {
		t.Fatalf("Failed to fetch saved line item: %v", err)
	}

	expectedTotal := 129.50 * 3.0 // 388.50
	if math.Abs(fetched.GetFloat("total_price")-expectedTotal) > 1e-4 {
		t.Errorf("Expected total_price %v, got %v", expectedTotal, fetched.GetFloat("total_price"))
	}

	expectedSummary := "Mechanical Keyboard x 3"
	if fetched.GetString("summary") != expectedSummary {
		t.Errorf("Expected summary %q, got %q", expectedSummary, fetched.GetString("summary"))
	}

	// 6. Update record: changing quantity must cause PostgreSQL to recompute generated columns
	fetched.Set("quantity", 5)
	if err := app.Save(fetched); err != nil {
		t.Fatalf("Failed to update line item: %v", err)
	}

	refetched, err := app.FindRecordById("order_line_items", "item00000000001")
	if err != nil {
		t.Fatalf("Failed to refetch updated line item: %v", err)
	}

	newExpectedTotal := 129.50 * 5.0 // 647.50
	if math.Abs(refetched.GetFloat("total_price")-newExpectedTotal) > 1e-4 {
		t.Errorf("Expected updated total_price %v, got %v", newExpectedTotal, refetched.GetFloat("total_price"))
	}

	newExpectedSummary := "Mechanical Keyboard x 5"
	if refetched.GetString("summary") != newExpectedSummary {
		t.Errorf("Expected updated summary %q, got %q", newExpectedSummary, refetched.GetString("summary"))
	}
}
