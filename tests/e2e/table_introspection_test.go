package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestTableIntrospection_DenyByDefault(t *testing.T) {
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

	// 1. Anonymous caller rejected with 401
	anonReq := httptest.NewRequest(http.MethodGet, "/api/ruxbase/tables", nil)
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, anonReq)
	if anonRec.Code != http.StatusUnauthorized {
		t.Errorf("Expected anonymous GET /api/ruxbase/tables to return 401, got %d: %s", anonRec.Code, anonRec.Body.String())
	}

	anonExposeReq := httptest.NewRequest(http.MethodPost, "/api/ruxbase/tables/expose", bytes.NewBufferString(`{"tableName":"test"}`))
	anonExposeReq.Header.Set("Content-Type", "application/json")
	anonExposeRec := httptest.NewRecorder()
	mux.ServeHTTP(anonExposeRec, anonExposeReq)
	if anonExposeRec.Code != http.StatusUnauthorized {
		t.Errorf("Expected anonymous POST /api/ruxbase/tables/expose to return 401, got %d: %s", anonExposeRec.Code, anonExposeRec.Body.String())
	}

	// 2. Regular authenticated user rejected with 403 Forbidden
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}
	user := core.NewRecord(usersCol)
	user.SetEmail("regular_user@example.com")
	user.SetPassword("validpassword123")
	user.SetVerified(true)
	if err := app.Save(user); err != nil {
		t.Fatalf("Failed to create regular user: %v", err)
	}

	userToken, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate user token: %v", err)
	}

	userReq := httptest.NewRequest(http.MethodGet, "/api/ruxbase/tables", nil)
	userReq.Header.Set("Authorization", userToken)
	userRec := httptest.NewRecorder()
	mux.ServeHTTP(userRec, userReq)
	if userRec.Code != http.StatusForbidden {
		t.Errorf("Expected regular user GET /api/ruxbase/tables to return 403, got %d: %s", userRec.Code, userRec.Body.String())
	}

	userExposeReq := httptest.NewRequest(http.MethodPost, "/api/ruxbase/tables/expose", bytes.NewBufferString(`{"tableName":"test"}`))
	userExposeReq.Header.Set("Authorization", userToken)
	userExposeReq.Header.Set("Content-Type", "application/json")
	userExposeRec := httptest.NewRecorder()
	mux.ServeHTTP(userExposeRec, userExposeReq)
	if userExposeRec.Code != http.StatusForbidden {
		t.Errorf("Expected regular user POST /api/ruxbase/tables/expose to return 403, got %d: %s", userExposeRec.Code, userExposeRec.Body.String())
	}
}

func TestTableIntrospection_SuperuserAuthorized(t *testing.T) {
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

	superusersCol, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("Failed to find superusers collection: %v", err)
	}
	admin := core.NewRecord(superusersCol)
	admin.SetEmail("admin@example.com")
	admin.SetPassword("adminpassword123")
	if err := app.Save(admin); err != nil {
		t.Fatalf("Failed to create admin record: %v", err)
	}

	adminToken, err := admin.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate admin token: %v", err)
	}

	// Create native table via direct SQL
	createSQL := `
		CREATE TABLE native_products (
			id text PRIMARY KEY,
			name varchar(255) NOT NULL,
			price numeric(10,2) DEFAULT 0.00,
			in_stock boolean DEFAULT true,
			created timestamptz DEFAULT now()
		);
	`
	if _, err := app.DB().NewQuery(createSQL).Execute(); err != nil {
		t.Fatalf("Failed to create native_products table: %v", err)
	}

	// 1. List tables
	listReq := httptest.NewRequest(http.MethodGet, "/api/ruxbase/tables", nil)
	listReq.Header.Set("Authorization", adminToken)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("Expected GET /api/ruxbase/tables to return 200, got %d: %s", listRec.Code, listRec.Body.String())
	}

	var tables []*core.NativeTableSummary
	if err := json.Unmarshal(listRec.Body.Bytes(), &tables); err != nil {
		t.Fatalf("Failed to decode tables response: %v", err)
	}

	var foundProduct bool
	for _, tbl := range tables {
		if tbl.Name == "native_products" {
			foundProduct = true
			if tbl.IsExposed {
				t.Errorf("native_products should not be marked isExposed before registration")
			}
			if !tbl.HasPrimaryKey {
				t.Errorf("native_products should have HasPrimaryKey true")
			}
			break
		}
	}
	if !foundProduct {
		t.Fatalf("native_products table not found in introspected tables list")
	}

	// 2. Detail table
	detailReq := httptest.NewRequest(http.MethodGet, "/api/ruxbase/tables/native_products", nil)
	detailReq.Header.Set("Authorization", adminToken)
	detailRec := httptest.NewRecorder()
	mux.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("Expected GET /api/ruxbase/tables/native_products to return 200, got %d: %s", detailRec.Code, detailRec.Body.String())
	}
}

func TestTableIntrospection_UnexposedTableUnreachableViaAPI(t *testing.T) {
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

	// Create native table via SQL
	if _, err := app.DB().NewQuery("CREATE TABLE unexposed_orders (id text PRIMARY KEY, customer text);").Execute(); err != nil {
		t.Fatalf("Failed to create unexposed_orders: %v", err)
	}

	// Querying records endpoint MUST return 404 Not Found
	req := httptest.NewRequest(http.MethodGet, "/api/collections/unexposed_orders/records", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected unexposed table to return 404 on records endpoint, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTableIntrospection_ExposeNativeTableAndQuery(t *testing.T) {
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

	// Admin token
	superusersCol, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("Failed to find superusers collection: %v", err)
	}
	admin := core.NewRecord(superusersCol)
	admin.SetEmail("admin2@example.com")
	admin.SetPassword("adminpassword123")
	if err := app.Save(admin); err != nil {
		t.Fatalf("Failed to create admin record: %v", err)
	}
	adminToken, err := admin.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate admin token: %v", err)
	}

	// Create native table and seed a row via direct SQL
	setupSQL := `
		CREATE TABLE native_gadgets (
			id text PRIMARY KEY,
			title varchar(200) NOT NULL,
			price numeric(10,2) DEFAULT 0.00
		);
		INSERT INTO native_gadgets (id, title, price) VALUES ('gadget_001', 'Smart Watch', 199.99);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to setup native_gadgets: %v", err)
	}

	// Expose table with ListRule = "@request.auth.id != ''"
	listRule := "@request.auth.id != ''"
	exposeBody, _ := json.Marshal(core.ExposeTableOptions{
		TableName:      "native_gadgets",
		CollectionName: "native_gadgets",
		ListRule:       &listRule,
		ViewRule:       &listRule,
	})

	exposeReq := httptest.NewRequest(http.MethodPost, "/api/ruxbase/tables/expose", bytes.NewBuffer(exposeBody))
	exposeReq.Header.Set("Authorization", adminToken)
	exposeReq.Header.Set("Content-Type", "application/json")
	exposeRec := httptest.NewRecorder()
	mux.ServeHTTP(exposeRec, exposeReq)

	if exposeRec.Code != http.StatusOK {
		t.Fatalf("Expected POST /api/ruxbase/tables/expose to return 200, got %d: %s", exposeRec.Code, exposeRec.Body.String())
	}

	// 1. Anonymous access returns 0 records under ListRule = "@request.auth.id != ''"
	anonReq := httptest.NewRequest(http.MethodGet, "/api/collections/native_gadgets/records", nil)
	anonRec := httptest.NewRecorder()
	mux.ServeHTTP(anonRec, anonReq)
	if anonRec.Code != http.StatusOK {
		t.Errorf("Expected anonymous query on native_gadgets to return 200, got %d: %s", anonRec.Code, anonRec.Body.String())
	}
	var anonResp struct {
		Total int `json:"totalItems"`
	}
	_ = json.Unmarshal(anonRec.Body.Bytes(), &anonResp)
	if anonResp.Total != 0 {
		t.Errorf("Expected anonymous query to return 0 items under ListRule, got %d", anonResp.Total)
	}

	// 2. Create record is denied with 403 Forbidden because createRule is nil (deny-by-default)
	createReq := httptest.NewRequest(http.MethodPost, "/api/collections/native_gadgets/records", bytes.NewBufferString(`{"id":"g2","title":"Tablet"}`))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	mux.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusForbidden {
		t.Errorf("Expected create on native_gadgets with nil createRule to return 403 Forbidden, got %d: %s", createRec.Code, createRec.Body.String())
	}

	// 2. Authenticated user access should succeed
	usersCol, _ := app.FindCollectionByNameOrId("users")
	user := core.NewRecord(usersCol)
	user.SetEmail("buyer@example.com")
	user.SetPassword("buyerpassword123")
	user.SetVerified(true)
	if err := app.Save(user); err != nil {
		t.Fatalf("Failed to create buyer user: %v", err)
	}
	userToken, _ := user.NewAuthToken()

	userReq := httptest.NewRequest(http.MethodGet, "/api/collections/native_gadgets/records", nil)
	userReq.Header.Set("Authorization", userToken)
	userRec := httptest.NewRecorder()
	mux.ServeHTTP(userRec, userReq)

	if userRec.Code != http.StatusOK {
		t.Fatalf("Expected authenticated query to return 200, got %d: %s", userRec.Code, userRec.Body.String())
	}

	// Verify returned record
	var recordsResp struct {
		Items []map[string]any `json:"items"`
		Total int              `json:"totalItems"`
	}
	if err := json.Unmarshal(userRec.Body.Bytes(), &recordsResp); err != nil {
		t.Fatalf("Failed to decode records response: %v", err)
	}

	if recordsResp.Total != 1 || len(recordsResp.Items) != 1 {
		t.Fatalf("Expected 1 record item, got %d", recordsResp.Total)
	}
	if recordsResp.Items[0]["id"] != "gadget_001" || recordsResp.Items[0]["title"] != "Smart Watch" {
		t.Errorf("Unexpected record contents: %v", recordsResp.Items[0])
	}
}

func TestTableIntrospection_RejectsSystemTables(t *testing.T) {
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

	superusersCol, _ := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	admin := core.NewRecord(superusersCol)
	admin.SetEmail("admin3@example.com")
	admin.SetPassword("adminpassword123")
	_ = app.Save(admin)
	adminToken, _ := admin.NewAuthToken()

	forbiddenTables := []string{
		"_collections",
		"_params",
		"_migrations",
		"pg_class",
		"pg_attribute",
	}

	for _, tbl := range forbiddenTables {
		t.Run(tbl, func(t *testing.T) {
			body, _ := json.Marshal(core.ExposeTableOptions{TableName: tbl})
			req := httptest.NewRequest(http.MethodPost, "/api/ruxbase/tables/expose", bytes.NewBuffer(body))
			req.Header.Set("Authorization", adminToken)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest {
				t.Errorf("Expected exposing %q to return 400 Bad Request, got %d: %s", tbl, rec.Code, rec.Body.String())
			}
		})
	}
}
