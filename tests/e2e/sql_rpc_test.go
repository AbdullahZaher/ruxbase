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

func TestSQLRPC_DenyByDefaultAndExposure(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create native PostgreSQL functions
	sqlFunc1 := `
		CREATE OR REPLACE FUNCTION calc_multiply(a integer, b integer)
		RETURNS integer LANGUAGE sql IMMUTABLE AS 'SELECT a * b';
	`
	if _, err := app.DB().NewQuery(sqlFunc1).Execute(); err != nil {
		t.Fatalf("Failed to create calc_multiply: %v", err)
	}

	sqlFunc2 := `
		CREATE OR REPLACE FUNCTION unexposed_secret_func(key text)
		RETURNS text LANGUAGE sql IMMUTABLE AS 'SELECT ''secret_'' || key';
	`
	if _, err := app.DB().NewQuery(sqlFunc2).Execute(); err != nil {
		t.Fatalf("Failed to create unexposed_secret_func: %v", err)
	}

	// 2. Negative check: unexposed function must return 404
	req := httptest.NewRequest(http.MethodPost, "/api/ruxbase/rpc/unexposed_secret_func", stringsReader(`{"key": "test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Expected 404 for unexposed function, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. Superuser discovery
	functions, err := app.ListRPCFunctions("public")
	if err != nil {
		t.Fatalf("ListRPCFunctions failed: %v", err)
	}

	var foundMultiply bool
	for _, fn := range functions {
		if fn.Name == "calc_multiply" {
			foundMultiply = true
			if fn.ReturnType != "integer" {
				t.Errorf("Expected return type integer, got %s", fn.ReturnType)
			}
			if len(fn.Parameters) != 2 {
				t.Errorf("Expected 2 parameters, got %d", len(fn.Parameters))
			}
			break
		}
	}
	if !foundMultiply {
		t.Errorf("calc_multiply function was not found in catalog")
	}

	// 4. Expose calc_multiply with public access rule ("")
	publicRule := ""
	_, err = app.ExposeRPCFunction(core.ExposeFunctionOptions{
		FunctionName: "calc_multiply",
		AccessRule:   &publicRule,
	})
	if err != nil {
		t.Fatalf("ExposeRPCFunction failed: %v", err)
	}

	// 5. Invoke exposed function via API as anonymous user
	reqBody := `{"a": 6, "b": 7}`
	req = httptest.NewRequest(http.MethodPost, "/api/ruxbase/rpc/calc_multiply", stringsReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for public RPC execution, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Result any `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	resultInt, ok := resp.Result.(float64) // JSON unmarshals numbers as float64
	if !ok || int(resultInt) != 42 {
		t.Fatalf("Expected result 42, got %v", resp.Result)
	}
}

func TestSQLRPC_AuthenticationAndAuthorization(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a function that requires authentication
	setupSQL := `
		CREATE OR REPLACE FUNCTION user_greeting(name text)
		RETURNS text LANGUAGE sql IMMUTABLE AS 'SELECT ''Hello, '' || name';
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create user_greeting: %v", err)
	}

	// Expose with rule requiring authentication
	authRule := "@request.auth.id != ''"
	_, err = app.ExposeRPCFunction(core.ExposeFunctionOptions{
		FunctionName: "user_greeting",
		AccessRule:   &authRule,
	})
	if err != nil {
		t.Fatalf("ExposeRPCFunction failed: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// 2. Anonymous call must return 401 Unauthorized
	req := httptest.NewRequest(http.MethodPost, "/api/ruxbase/rpc/user_greeting", stringsReader(`{"name": "Alice"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("Expected 401 Unauthorized for unauthenticated RPC, got %d: %s", rec.Code, rec.Body.String())
	}

	// 3. Create auth user and obtain token
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}

	user := core.NewRecord(usersCol)
	user.Set("email", "rpcuser@example.com")
	user.Set("password", "TestPassword123!")
	if err := app.Save(user); err != nil {
		t.Fatalf("Failed to create test user: %v", err)
	}

	token, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate token: %v", err)
	}

	// 4. Authenticated call must succeed with 200 OK
	req = httptest.NewRequest(http.MethodPost, "/api/ruxbase/rpc/user_greeting", stringsReader(`{"name": "Alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)
	rec = httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for authenticated RPC, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		Result string `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if resp.Result != "Hello, Alice" {
		t.Errorf("Expected 'Hello, Alice', got %q", resp.Result)
	}
}

func TestSQLRPC_SetReturningFunction(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a function returning a table (multiple rows)
	setupSQL := `
		CREATE OR REPLACE FUNCTION get_system_metrics(threshold integer)
		RETURNS TABLE (metric_name text, metric_value integer)
		LANGUAGE sql STABLE AS $$
			SELECT 'cpu'::text, 45
			UNION ALL
			SELECT 'memory'::text, 80
			UNION ALL
			SELECT 'disk'::text, 25
			WHERE 25 >= threshold;
		$$;
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create get_system_metrics: %v", err)
	}

	publicRule := ""
	_, err = app.ExposeRPCFunction(core.ExposeFunctionOptions{
		FunctionName: "get_system_metrics",
		AccessRule:   &publicRule,
	})
	if err != nil {
		t.Fatalf("ExposeRPCFunction failed: %v", err)
	}

	// Execute RPC directly via app
	result, err := app.ExecuteRPC("get_system_metrics", map[string]any{"threshold": 10}, nil, false)
	if err != nil {
		t.Fatalf("ExecuteRPC failed: %v", err)
	}

	rows, ok := result.([]map[string]any)
	if !ok {
		t.Fatalf("Expected []map[string]any for table-returning function, got %T: %v", result, result)
	}

	if len(rows) != 3 {
		t.Fatalf("Expected 3 metric rows, got %d", len(rows))
	}
}

func stringsReader(s string) *bytes.Reader {
	return bytes.NewReader([]byte(s))
}
