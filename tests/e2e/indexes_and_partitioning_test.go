package e2e_test

import (
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

func TestReviewedIndexes_CreationIntrospectionAndDrop(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a table with diverse column types
	setupSQL := `
		CREATE TABLE catalog_items (
			id text PRIMARY KEY,
			sku text NOT NULL,
			attributes jsonb DEFAULT '{}'::jsonb,
			location point,
			price numeric(10, 2) NOT NULL,
			is_active boolean DEFAULT true
		);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create catalog_items: %v", err)
	}

	// 2. Create B-Tree unique index on sku
	err = app.CreateNativeIndex(core.CreateIndexOptions{
		TableName: "catalog_items",
		IndexName: "idx_catalog_sku_unique",
		Method:    "btree",
		Columns:   []string{"sku"},
		Unique:    true,
	})
	if err != nil {
		t.Fatalf("Failed to create btree index: %v", err)
	}

	// 3. Create GIN index on jsonb attributes
	err = app.CreateNativeIndex(core.CreateIndexOptions{
		TableName: "catalog_items",
		IndexName: "idx_catalog_attrs_gin",
		Method:    "gin",
		Columns:   []string{"attributes"},
	})
	if err != nil {
		t.Fatalf("Failed to create gin index: %v", err)
	}

	// 4. Create GiST index on location point
	err = app.CreateNativeIndex(core.CreateIndexOptions{
		TableName: "catalog_items",
		IndexName: "idx_catalog_loc_gist",
		Method:    "gist",
		Columns:   []string{"location"},
	})
	if err != nil {
		t.Fatalf("Failed to create gist index: %v", err)
	}

	// 5. Create Partial B-Tree index on price WHERE is_active = true
	err = app.CreateNativeIndex(core.CreateIndexOptions{
		TableName: "catalog_items",
		IndexName: "idx_catalog_active_price",
		Method:    "btree",
		Columns:   []string{"price"},
		Predicate: "is_active = true",
	})
	if err != nil {
		t.Fatalf("Failed to create partial index: %v", err)
	}

	// 6. Introspect indexes and verify catalog reflection
	indexes, err := app.IntrospectTableIndexes("catalog_items")
	if err != nil {
		t.Fatalf("IntrospectTableIndexes failed: %v", err)
	}

	indexMap := make(map[string]*core.NativeIndexInfo)
	for _, idx := range indexes {
		indexMap[idx.Name] = idx
	}

	// Check btree unique
	skuIdx, ok := indexMap["idx_catalog_sku_unique"]
	if !ok {
		t.Errorf("Index idx_catalog_sku_unique not found")
	} else {
		if !skuIdx.IsUnique {
			t.Errorf("Expected sku index to be unique")
		}
		if skuIdx.Method != "btree" {
			t.Errorf("Expected method btree, got %s", skuIdx.Method)
		}
	}

	// Check gin
	ginIdx, ok := indexMap["idx_catalog_attrs_gin"]
	if !ok {
		t.Errorf("Index idx_catalog_attrs_gin not found")
	} else if ginIdx.Method != "gin" {
		t.Errorf("Expected method gin, got %s", ginIdx.Method)
	}

	// Check gist
	gistIdx, ok := indexMap["idx_catalog_loc_gist"]
	if !ok {
		t.Errorf("Index idx_catalog_loc_gist not found")
	} else if gistIdx.Method != "gist" {
		t.Errorf("Expected method gist, got %s", gistIdx.Method)
	}

	// Check partial index predicate
	partialIdx, ok := indexMap["idx_catalog_active_price"]
	if !ok {
		t.Errorf("Index idx_catalog_active_price not found")
	} else if !strings.Contains(strings.ToLower(partialIdx.Definition), "is_active = true") {
		t.Errorf("Expected WHERE clause in index definition, got %s", partialIdx.Definition)
	}

	// 7. Drop index and verify removal
	if err := app.DropNativeIndex("idx_catalog_attrs_gin"); err != nil {
		t.Fatalf("DropNativeIndex failed: %v", err)
	}

	afterDropIndexes, err := app.IntrospectTableIndexes("catalog_items")
	if err != nil {
		t.Fatalf("IntrospectTableIndexes after drop failed: %v", err)
	}
	for _, idx := range afterDropIndexes {
		if idx.Name == "idx_catalog_attrs_gin" {
			t.Errorf("Dropped index idx_catalog_attrs_gin still found in catalog")
		}
	}

	// 8. Negative check: reject unapproved index method
	err = app.CreateNativeIndex(core.CreateIndexOptions{
		TableName: "catalog_items",
		IndexName: "idx_catalog_bad",
		Method:    "unsupported_method",
		Columns:   []string{"price"},
	})
	if err == nil {
		t.Fatalf("Expected CreateNativeIndex with unsupported method to fail, but succeeded")
	}
}

func TestPostgreSQLExtensions_WhitelistAndLifecycle(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. List available extensions
	available, err := app.ListAvailableExtensions()
	if err != nil {
		t.Fatalf("ListAvailableExtensions failed: %v", err)
	}
	if len(available) == 0 {
		t.Fatalf("Expected at least one available extension in PostgreSQL cluster")
	}

	// Verify vector is in available list
	var foundVector bool
	for _, ext := range available {
		if ext.Name == "vector" {
			foundVector = true
			break
		}
	}
	if !foundVector {
		t.Errorf("Expected vector extension in available extensions list")
	}

	// 2. Enable approved extension (pg_trgm)
	if err := app.EnableExtension("pg_trgm"); err != nil {
		t.Fatalf("Failed to enable approved extension pg_trgm: %v", err)
	}

	// Verify pg_trgm is in installed list
	installed, err := app.ListInstalledExtensions()
	if err != nil {
		t.Fatalf("ListInstalledExtensions failed: %v", err)
	}

	var foundTrgm bool
	for _, ext := range installed {
		if ext.Name == "pg_trgm" {
			foundTrgm = true
			break
		}
	}
	if !foundTrgm {
		t.Errorf("Expected pg_trgm in installed extensions list")
	}

	// 3. Negative check: reject unapproved extension
	err = app.EnableExtension("plpython3u")
	if err == nil {
		t.Fatalf("Expected EnableExtension with unapproved extension plpython3u to fail, but succeeded")
	}
	if !strings.Contains(err.Error(), "unapproved") {
		t.Errorf("Expected unapproved extension error message, got: %v", err)
	}

	// Negative check: reject malicious SQL injection in extension name
	err = app.EnableExtension("vector; DROP TABLE users;")
	if err == nil {
		t.Fatalf("Expected SQL injection in extension name to fail, but succeeded")
	}
}

func TestDeclarativePartitioning_IntrospectionAndRouting(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a partitioned table partitioned by RANGE on created_at
	setupSQL := `
		CREATE TABLE sensor_readings (
			id text NOT NULL,
			device_id text NOT NULL,
			reading numeric(8, 2) NOT NULL,
			created_at timestamptz NOT NULL,
			PRIMARY KEY (id, created_at)
		) PARTITION BY RANGE (created_at);

		CREATE TABLE sensor_readings_2026_01 PARTITION OF sensor_readings
			FOR VALUES FROM ('2026-01-01 00:00:00+00') TO ('2026-02-01 00:00:00+00');

		CREATE TABLE sensor_readings_2026_02 PARTITION OF sensor_readings
			FOR VALUES FROM ('2026-02-01 00:00:00+00') TO ('2026-03-01 00:00:00+00');
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create partitioned sensor_readings table: %v", err)
	}

	// 2. Introspect partition metadata
	partInfo, err := app.IntrospectPartitionInfo("sensor_readings")
	if err != nil {
		t.Fatalf("IntrospectPartitionInfo failed: %v", err)
	}

	if partInfo.Strategy != "RANGE" {
		t.Errorf("Expected partition strategy RANGE, got %q", partInfo.Strategy)
	}
	if len(partInfo.KeyColumns) != 1 || partInfo.KeyColumns[0] != "created_at" {
		t.Errorf("Expected partition key created_at, got %v", partInfo.KeyColumns)
	}
	if len(partInfo.Partitions) != 2 {
		t.Fatalf("Expected 2 child partitions, got %d: %v", len(partInfo.Partitions), partInfo.Partitions)
	}

	// 3. Expose partitioned table as Collection
	listRule := ""
	col, err := app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "sensor_readings",
		CollectionName: "sensor_readings",
		ListRule:       &listRule,
	})
	if err != nil {
		t.Fatalf("ExposeNativeTable on partitioned table failed: %v", err)
	}

	// 4. Insert records into parent table and verify PostgreSQL routes to correct partitions
	timeJan := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	dtJan, _ := types.ParseDateTime(timeJan)

	recJan := core.NewRecord(col)
	recJan.Set("id", "sensor000000001")
	recJan.Set("device_id", "dev_100")
	recJan.Set("reading", 42.50)
	recJan.Set("created_at", dtJan)
	if err := app.Save(recJan); err != nil {
		t.Fatalf("Failed to insert reading into January partition: %v", err)
	}

	timeFeb := time.Date(2026, 2, 20, 15, 30, 0, 0, time.UTC)
	dtFeb, _ := types.ParseDateTime(timeFeb)

	recFeb := core.NewRecord(col)
	recFeb.Set("id", "sensor000000002")
	recFeb.Set("device_id", "dev_200")
	recFeb.Set("reading", 98.75)
	recFeb.Set("created_at", dtFeb)
	if err := app.Save(recFeb); err != nil {
		t.Fatalf("Failed to insert reading into February partition: %v", err)
	}

	// 5. Verify direct row counts in each child partition via SQL
	var janCount int
	err = app.DB().NewQuery("SELECT count(*) FROM sensor_readings_2026_01").Row(&janCount)
	if err != nil || janCount != 1 {
		t.Errorf("Expected exactly 1 row in sensor_readings_2026_01, got %d (err: %v)", janCount, err)
	}

	var febCount int
	err = app.DB().NewQuery("SELECT count(*) FROM sensor_readings_2026_02").Row(&febCount)
	if err != nil || febCount != 1 {
		t.Errorf("Expected exactly 1 row in sensor_readings_2026_02, got %d (err: %v)", febCount, err)
	}

	// 6. Verify unified query across partitioned table returns both records
	totalCount, err := app.CountRecords(col)
	if err != nil || totalCount != 2 {
		t.Errorf("Expected 2 total records from partitioned table query, got %d (err: %v)", totalCount, err)
	}
}
