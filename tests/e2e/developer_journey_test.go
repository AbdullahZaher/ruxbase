package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/cmd"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestDeveloperJourney_FullLifecycle(t *testing.T) {
	// Step 1: Preflight environment and diagnostic check
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	checkCmd := cmd.NewCheckCommand(app)
	checkCmd.SetArgs([]string{"--json"})
	if err := checkCmd.Execute(); err != nil {
		t.Fatalf("Step 1 failed: Preflight check failed: %v", err)
	}

	// Step 2: Initialize router and build HTTP mux
	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Step 2 failed: Router creation failed: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Step 2 failed: Mux build failed: %v", err)
	}

	// Step 3: Create native table and expose it via API
	setupSQL := `
		CREATE TABLE developer_tasks (
			id text PRIMARY KEY,
			title varchar(255) NOT NULL,
			priority int DEFAULT 1,
			completed boolean DEFAULT false,
			tenant_id text
		);
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Step 3 failed: native table creation: %v", err)
	}

	authRule := "@request.auth.id != ''"
	_, err = app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "developer_tasks",
		CollectionName: "developer_tasks",
		ListRule:       &authRule,
		CreateRule:     &authRule,
		DeleteRule:     &authRule,
	})
	if err != nil {
		t.Fatalf("Step 3 failed: expose native table: %v", err)
	}

	// Step 4: Verify unauthenticated caller is rejected with normalized error
	reqAnon := httptest.NewRequest(http.MethodGet, "/api/ruxbase/sql/tables/developer_tasks", nil)
	recAnon := httptest.NewRecorder()
	mux.ServeHTTP(recAnon, reqAnon)
	if recAnon.Code != http.StatusUnauthorized {
		t.Errorf("Step 4 failed: expected 401 for anonymous access, got %d", recAnon.Code)
	}

	// Step 5: Authenticate a user
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Step 5 failed: find users collection: %v", err)
	}
	devUser := core.NewRecord(usersCol)
	devUser.SetEmail("fresh_developer@example.com")
	devUser.SetPassword("validpassword123")
	devUser.SetVerified(true)
	if err := app.Save(devUser); err != nil {
		t.Fatalf("Step 5 failed: create user: %v", err)
	}
	userToken, err := devUser.NewAuthToken()
	if err != nil {
		t.Fatalf("Step 5 failed: generate token: %v", err)
	}

	// Step 6: Perform bounded bulk insert
	var taskRows []map[string]any
	for i := 1; i <= 5; i++ {
		taskRows = append(taskRows, map[string]any{
			"id":        fmt.Sprintf("task_%02d", i),
			"title":     fmt.Sprintf("Journey Task #%d", i),
			"priority":  i,
			"completed": false,
		})
	}
	principal := core.NewUserPrincipal(devUser.Id, "users", "", nil)
	bulkRes, err := app.BulkInsertExposedTable(principal, "developer_tasks", taskRows)
	if err != nil {
		t.Fatalf("Step 6 failed: bulk insert: %v", err)
	}
	if bulkRes.Affected != 5 {
		t.Errorf("Step 6 failed: expected 5 affected rows, got %d", bulkRes.Affected)
	}

	// Step 7: Query exposed table with pagination and sort
	queryReq := httptest.NewRequest(http.MethodGet, "/api/ruxbase/sql/tables/developer_tasks?page=1&perPage=3&sort=-priority", nil)
	queryReq.Header.Set("Authorization", userToken)
	queryRec := httptest.NewRecorder()
	mux.ServeHTTP(queryRec, queryReq)

	if queryRec.Code != http.StatusOK {
		t.Fatalf("Step 7 failed: query exposed table returned %d: %s", queryRec.Code, queryRec.Body.String())
	}

	var pageRes core.TablePaginatedResult
	if err := json.Unmarshal(queryRec.Body.Bytes(), &pageRes); err != nil {
		t.Fatalf("Step 7 failed: parse query result: %v", err)
	}
	if pageRes.TotalItems != 5 {
		t.Errorf("Step 7 failed: expected 5 totalItems, got %d", pageRes.TotalItems)
	}
	if len(pageRes.Items) != 3 {
		t.Errorf("Step 7 failed: expected 3 items in page, got %d", len(pageRes.Items))
	}
	// Highest priority is task 5
	if pageRes.Items[0]["id"] != "task_05" {
		t.Errorf("Step 7 failed: expected first item task_05, got %v", pageRes.Items[0]["id"])
	}

	// Step 8: Bulk delete
	delRes, err := app.BulkDeleteExposedTable(principal, "developer_tasks", []string{"task_01", "task_02"})
	if err != nil {
		t.Fatalf("Step 8 failed: bulk delete: %v", err)
	}
	if delRes.Affected != 2 {
		t.Errorf("Step 8 failed: expected 2 deleted rows, got %d", delRes.Affected)
	}

	// Verify 3 remaining items
	var remaining int
	_ = app.DB().NewQuery("SELECT COUNT(*) FROM developer_tasks").Row(&remaining)
	if remaining != 3 {
		t.Errorf("Step 8 failed: expected 3 remaining items, got %d", remaining)
	}
}
