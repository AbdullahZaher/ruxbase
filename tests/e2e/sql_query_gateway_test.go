package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestSQLQueryGateway_ExposedTableQueries(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// 1. Unexposed table returns 404
	req := httptest.NewRequest(http.MethodGet, "/api/ruxbase/sql/tables/non_existent_or_unexposed", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for unexposed table, got %d: %s", rec.Code, rec.Body.String())
	}

	// 2. Create native table and seed rows
	setupSQL := `
		CREATE TABLE native_products (
			id text PRIMARY KEY,
			title varchar(200) NOT NULL,
			price numeric(10,2) DEFAULT 0.00,
			tenant_id text
		);
		INSERT INTO native_products (id, title, price) VALUES 
			('prod_001', 'Keyboard', 49.99),
			('prod_002', 'Mouse', 29.99),
			('prod_003', 'Monitor', 299.99),
			('prod_004', 'Headphones', 79.99);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to setup native_products: %v", err)
	}

	// Expose table with ListRule requiring authenticated user
	authRule := "@request.auth.id != ''"
	_, err = app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "native_products",
		CollectionName: "native_products",
		ListRule:       &authRule,
		CreateRule:     &authRule,
		DeleteRule:     &authRule,
	})
	if err != nil {
		t.Fatalf("Failed to expose native_products: %v", err)
	}

	// 3. Anonymous caller should be rejected (401)
	anonReq := httptest.NewRequest(http.MethodGet, "/api/ruxbase/sql/tables/native_products", nil)
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, anonReq)
	if anonRec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 for anonymous access to authenticated-only table, got %d: %s", anonRec.Code, anonRec.Body.String())
	}

	// 4. Authenticated user can query with pagination and sorting
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}
	user := core.NewRecord(usersCol)
	user.SetEmail("query_user@example.com")
	user.SetPassword("validpassword123")
	user.SetVerified(true)
	if err := app.Save(user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userToken, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate user token: %v", err)
	}

	// Query page 1, perPage 2, sorted by price descending
	authReq := httptest.NewRequest(http.MethodGet, "/api/ruxbase/sql/tables/native_products?page=1&perPage=2&sort=-price", nil)
	authReq.Header.Set("Authorization", userToken)
	authRec := httptest.NewRecorder()
	mux.ServeHTTP(authRec, authReq)

	if authRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for authenticated query, got %d: %s", authRec.Code, authRec.Body.String())
	}

	var res core.TablePaginatedResult
	if err := json.Unmarshal(authRec.Body.Bytes(), &res); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if res.TotalItems != 4 {
		t.Errorf("Expected 4 totalItems, got %d", res.TotalItems)
	}
	if res.TotalPages != 2 {
		t.Errorf("Expected 2 totalPages, got %d", res.TotalPages)
	}
	if len(res.Items) != 2 {
		t.Fatalf("Expected 2 items in page, got %d", len(res.Items))
	}
	// Highest price is Monitor (299.99)
	if res.Items[0]["id"] != "prod_003" {
		t.Errorf("Expected first item to be Monitor (prod_003), got %v", res.Items[0]["id"])
	}
}

func TestSQLQueryGateway_BoundedBulkOperations(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// Create and expose native table
	setupSQL := `
		CREATE TABLE bulk_items (
			id text PRIMARY KEY,
			title text NOT NULL,
			tenant_id text
		);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create bulk_items table: %v", err)
	}

	authRule := "@request.auth.id != ''"
	_, err = app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "bulk_items",
		CollectionName: "bulk_items",
		ListRule:       &authRule,
		CreateRule:     &authRule,
		DeleteRule:     &authRule,
	})
	if err != nil {
		t.Fatalf("Failed to expose bulk_items: %v", err)
	}

	// Create authenticated user
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}
	user := core.NewRecord(usersCol)
	user.SetEmail("bulk_user@example.com")
	user.SetPassword("validpassword123")
	user.SetVerified(true)
	if err := app.Save(user); err != nil {
		t.Fatalf("Failed to create user: %v", err)
	}
	userToken, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// 1. Bulk insert within limit (10 items)
	var validBatch []map[string]any
	for i := 1; i <= 10; i++ {
		validBatch = append(validBatch, map[string]any{
			"id":    fmt.Sprintf("item_%03d", i),
			"title": fmt.Sprintf("Bulk Item #%d", i),
		})
	}

	batchBytes, _ := json.Marshal(validBatch)
	req := httptest.NewRequest(http.MethodPost, "/api/ruxbase/sql/tables/bulk_items/bulk", bytes.NewReader(batchBytes))
	req.Header.Set("Authorization", userToken)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for bulk insert, got %d: %s", rec.Code, rec.Body.String())
	}

	var bulkRes core.BulkResult
	if err := json.Unmarshal(rec.Body.Bytes(), &bulkRes); err != nil {
		t.Fatalf("Failed to unmarshal bulk result: %v", err)
	}
	if bulkRes.Affected != 10 {
		t.Errorf("Expected 10 affected rows, got %d", bulkRes.Affected)
	}
	if len(bulkRes.Ids) != 10 {
		t.Errorf("Expected 10 IDs returned, got %d", len(bulkRes.Ids))
	}

	// 2. Bulk insert exceeding max bound (> 500 items)
	var oversizedBatch []map[string]any
	for i := 1; i <= 501; i++ {
		oversizedBatch = append(oversizedBatch, map[string]any{
			"id":    fmt.Sprintf("oversized_%d", i),
			"title": fmt.Sprintf("Oversized Item #%d", i),
		})
	}
	overBytes, _ := json.Marshal(oversizedBatch)
	overReq := httptest.NewRequest(http.MethodPost, "/api/ruxbase/sql/tables/bulk_items/bulk", bytes.NewReader(overBytes))
	overReq.Header.Set("Authorization", userToken)
	overReq.Header.Set("Content-Type", "application/json")
	overRec := httptest.NewRecorder()
	mux.ServeHTTP(overRec, overReq)

	if overRec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for bulk insert > 500 items, got %d: %s", overRec.Code, overRec.Body.String())
	}

	// 3. Bulk delete within limit (5 items)
	delPayload := map[string]any{
		"ids": []string{"item_001", "item_002", "item_003", "item_004", "item_005"},
	}
	delBytes, _ := json.Marshal(delPayload)
	delReq := httptest.NewRequest(http.MethodDelete, "/api/ruxbase/sql/tables/bulk_items/bulk", bytes.NewReader(delBytes))
	delReq.Header.Set("Authorization", userToken)
	delReq.Header.Set("Content-Type", "application/json")
	delRec := httptest.NewRecorder()
	mux.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for bulk delete, got %d: %s", delRec.Code, delRec.Body.String())
	}

	var delRes core.BulkResult
	if err := json.Unmarshal(delRec.Body.Bytes(), &delRes); err != nil {
		t.Fatalf("Failed to parse delete result: %v", err)
	}
	if delRes.Affected != 5 {
		t.Errorf("Expected 5 affected rows in delete, got %d", delRes.Affected)
	}

	// Verify remaining items in database
	var remainingCount int
	if err := app.DB().NewQuery("SELECT COUNT(*) FROM bulk_items").Row(&remainingCount); err != nil {
		t.Fatalf("Failed to count remaining: %v", err)
	}
	if remainingCount != 5 {
		t.Errorf("Expected 5 remaining items, got %d", remainingCount)
	}
}

func TestSQLQueryGateway_TenantIsolation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Initialize IAM
	if err := app.EnsureIAMSystemTables(); err != nil {
		t.Fatalf("Failed to initialize IAM: %v", err)
	}

	// 2. Create two tenants
	tenantA, err := app.CreateTenant("Tenant Alpha", "tenant-alpha")
	if err != nil {
		t.Fatalf("Failed to create tenant A: %v", err)
	}
	tenantB, err := app.CreateTenant("Tenant Beta", "tenant-beta")
	if err != nil {
		t.Fatalf("Failed to create tenant B: %v", err)
	}

	// 3. Create native table and enable tenant RLS
	setupSQL := `
		CREATE TABLE tenant_scoped_notes (
			id text PRIMARY KEY,
			title text NOT NULL,
			tenant_id text NOT NULL
		);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to setup tenant_scoped_notes: %v", err)
	}

	if err := app.EnableTenantRLS("tenant_scoped_notes"); err != nil {
		t.Fatalf("Failed to enable tenant RLS: %v", err)
	}

	authRule := "@request.auth.id != ''"
	_, err = app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "tenant_scoped_notes",
		CollectionName: "tenant_scoped_notes",
		ListRule:       &authRule,
		CreateRule:     &authRule,
		DeleteRule:     &authRule,
	})
	if err != nil {
		t.Fatalf("Failed to expose tenant_scoped_notes: %v", err)
	}

	// Create service accounts for both tenants
	_, rawKeyA, err := app.CreateServiceAccount("svc-a", tenantA.Id, "Scope A", []string{"*"}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create svc A: %v", err)
	}
	principalA, err := app.AuthenticateServiceAccount(rawKeyA, tenantA.Id)
	if err != nil {
		t.Fatalf("Failed to authenticate principal A: %v", err)
	}

	_, rawKeyB, err := app.CreateServiceAccount("svc-b", tenantB.Id, "Scope B", []string{"*"}, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create svc B: %v", err)
	}
	principalB, err := app.AuthenticateServiceAccount(rawKeyB, tenantB.Id)
	if err != nil {
		t.Fatalf("Failed to authenticate principal B: %v", err)
	}

	// Tenant A inserts rows
	rowsA := []map[string]any{
		{"id": "note_a1", "title": "Secret Note Alpha 1"},
		{"id": "note_a2", "title": "Secret Note Alpha 2"},
	}
	insRes, err := app.BulkInsertExposedTable(principalA, "tenant_scoped_notes", rowsA)
	if err != nil {
		t.Fatalf("Tenant A bulk insert failed: %v", err)
	}
	if insRes.Affected != 2 {
		t.Errorf("Expected 2 inserted rows for A, got %d", insRes.Affected)
	}

	// Tenant B queries the table -> must see 0 rows
	queryResB, err := app.QueryExposedTable(principalB, "tenant_scoped_notes", core.TableQueryParams{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("Tenant B query failed: %v", err)
	}
	if queryResB.TotalItems != 0 || len(queryResB.Items) != 0 {
		t.Errorf("Cross-tenant leakage! Tenant B saw %d items belonging to Tenant A", queryResB.TotalItems)
	}

	// Tenant A queries the table -> sees 2 rows
	queryResA, err := app.QueryExposedTable(principalA, "tenant_scoped_notes", core.TableQueryParams{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("Tenant A query failed: %v", err)
	}
	if queryResA.TotalItems != 2 {
		t.Errorf("Tenant A expected 2 items, got %d", queryResA.TotalItems)
	}
}
