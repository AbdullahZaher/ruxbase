package e2e_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestRealtime_OfflineDeltaSync(t *testing.T) {
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

	// 1. Create collection for sync test
	col := core.NewBaseCollection("offline_docs")
	emptyRule := ""
	col.ListRule = &emptyRule
	col.ViewRule = &emptyRule
	col.CreateRule = &emptyRule
	col.UpdateRule = &emptyRule
	col.DeleteRule = &emptyRule
	col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
	if err := app.Save(col); err != nil {
		t.Fatalf("Failed to save collection: %v", err)
	}

	// 2. Insert baseline records
	rec1 := core.NewRecord(col)
	rec1.Set("id", "doc000000000001")
	if err := app.Save(rec1); err != nil {
		t.Fatalf("Failed to save rec1: %v", err)
	}

	rec2 := core.NewRecord(col)
	rec2.Set("id", "doc000000000002")
	if err := app.Save(rec2); err != nil {
		t.Fatalf("Failed to save rec2: %v", err)
	}

	// 3. Baseline sync: GET /api/realtime/sync?collection=offline_docs
	reqBaseline := httptest.NewRequest(http.MethodGet, "/api/realtime/sync?collection=offline_docs", nil)
	recBaseline := httptest.NewRecorder()
	mux.ServeHTTP(recBaseline, reqBaseline)

	if recBaseline.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d: %s", recBaseline.Code, recBaseline.Body.String())
	}

	var baselineResp struct {
		Collection string           `json:"collection"`
		SyncedAt   string           `json:"syncedAt"`
		Records    []map[string]any `json:"records"`
		Deleted    []string         `json:"deleted"`
	}
	if err := json.Unmarshal(recBaseline.Body.Bytes(), &baselineResp); err != nil {
		t.Fatalf("Failed to decode baseline sync response: %v", err)
	}

	if len(baselineResp.Records) != 2 {
		t.Fatalf("Expected 2 baseline records, got %d", len(baselineResp.Records))
	}
	cursor := baselineResp.SyncedAt

	// 4. Client goes offline. While offline, server updates state:
	// - Delete doc 1 (produces tombstone in _realtime_events)
	if err := app.Delete(rec1); err != nil {
		t.Fatalf("Failed to delete rec1: %v", err)
	}

	// - Create doc 3
	rec3 := core.NewRecord(col)
	rec3.Set("id", "doc000000000003")
	if err := app.Save(rec3); err != nil {
		t.Fatalf("Failed to save rec3: %v", err)
	}

	// 5. Client reconnects and requests delta sync since cursor
	reqDelta := httptest.NewRequest(http.MethodGet, "/api/realtime/sync?collection=offline_docs&since="+cursor, nil)
	recDelta := httptest.NewRecorder()
	mux.ServeHTTP(recDelta, reqDelta)

	if recDelta.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d: %s", recDelta.Code, recDelta.Body.String())
	}

	var deltaResp struct {
		Collection string           `json:"collection"`
		SyncedAt   string           `json:"syncedAt"`
		Records    []map[string]any `json:"records"`
		Deleted    []string         `json:"deleted"`
	}
	if err := json.Unmarshal(recDelta.Body.Bytes(), &deltaResp); err != nil {
		t.Fatalf("Failed to decode delta sync response: %v", err)
	}

	// Should contain doc3 in records
	foundDoc3 := false
	for _, r := range deltaResp.Records {
		if r["id"] == "doc000000000003" {
			foundDoc3 = true
		}
	}
	if !foundDoc3 {
		t.Fatalf("Expected doc000000000003 in delta records, got: %+v", deltaResp.Records)
	}

	// Should contain doc1 in deleted
	foundDeletedDoc1 := false
	for _, id := range deltaResp.Deleted {
		if id == "doc000000000001" {
			foundDeletedDoc1 = true
		}
	}
	if !foundDeletedDoc1 {
		t.Fatalf("Expected doc000000000001 in delta deleted IDs, got: %+v", deltaResp.Deleted)
	}
}

func TestRealtime_OfflineBatchSyncLWW(t *testing.T) {
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

	col := core.NewBaseCollection("tasks")
	emptyRule := ""
	col.ListRule = &emptyRule
	col.ViewRule = &emptyRule
	col.CreateRule = &emptyRule
	col.UpdateRule = &emptyRule
	col.DeleteRule = &emptyRule
	col.Fields.Add(&core.TextField{Name: "title"})
	col.Fields.Add(&core.AutodateField{Name: "created", OnCreate: true})
	col.Fields.Add(&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true})
	if err := app.Save(col); err != nil {
		t.Fatalf("Failed to save collection: %v", err)
	}

	// Existing record
	task1 := core.NewRecord(col)
	task1.Set("id", "task00000000001")
	task1.Set("title", "Server Baseline Title")
	if err := app.Save(task1); err != nil {
		t.Fatalf("Failed to save task1: %v", err)
	}

	// Stale client time (1 hour in past)
	staleClientTime := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)
	// Fresh client time (1 hour in future)
	freshClientTime := time.Now().Add(1 * time.Hour).UTC().Format(time.RFC3339)

	// Test 1: Stale update rejected via Last-Write-Wins (server won)
	staleBatch := fmt.Sprintf(`{
		"collection": "tasks",
		"mutations": [
			{
				"action": "update",
				"id": "task00000000001",
				"data": {"title": "Stale Title Attempt"},
				"clientUpdatedAt": "%s"
			}
		]
	}`, staleClientTime)

	reqStale := httptest.NewRequest(http.MethodPost, "/api/realtime/sync", bytes.NewBufferString(staleBatch))
	reqStale.Header.Set("Content-Type", "application/json")
	recStale := httptest.NewRecorder()
	mux.ServeHTTP(recStale, reqStale)

	if recStale.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d: %s", recStale.Code, recStale.Body.String())
	}

	var staleResp struct {
		Applied   []map[string]any `json:"applied"`
		Conflicts []map[string]any `json:"conflicts"`
	}
	_ = json.Unmarshal(recStale.Body.Bytes(), &staleResp)

	if len(staleResp.Conflicts) != 1 || staleResp.Conflicts[0]["status"] != "conflict_server_won" {
		t.Fatalf("Expected 1 conflict_server_won, got: %+v", staleResp.Conflicts)
	}

	// Verify server title remained unchanged
	refreshedTask1, _ := app.FindRecordById("tasks", "task00000000001")
	if refreshedTask1.GetString("title") != "Server Baseline Title" {
		t.Fatalf("Expected title to remain 'Server Baseline Title', got '%s'", refreshedTask1.GetString("title"))
	}

	// Test 2: Fresh update and create applied via Last-Write-Wins
	freshBatch := fmt.Sprintf(`{
		"collection": "tasks",
		"mutations": [
			{
				"action": "update",
				"id": "task00000000001",
				"data": {"title": "Winning Fresh Title"},
				"clientUpdatedAt": "%s"
			},
			{
				"action": "create",
				"id": "task00000000002",
				"data": {"title": "Created While Offline"},
				"clientUpdatedAt": "%s"
			}
		]
	}`, freshClientTime, freshClientTime)

	reqFresh := httptest.NewRequest(http.MethodPost, "/api/realtime/sync", bytes.NewBufferString(freshBatch))
	reqFresh.Header.Set("Content-Type", "application/json")
	recFresh := httptest.NewRecorder()
	mux.ServeHTTP(recFresh, reqFresh)

	if recFresh.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d: %s", recFresh.Code, recFresh.Body.String())
	}

	var freshResp struct {
		Applied   []map[string]any `json:"applied"`
		Conflicts []map[string]any `json:"conflicts"`
	}
	_ = json.Unmarshal(recFresh.Body.Bytes(), &freshResp)

	if len(freshResp.Applied) != 2 {
		t.Fatalf("Expected 2 applied mutations, got: %+v", freshResp.Applied)
	}

	// Verify updates applied in database
	updatedTask1, _ := app.FindRecordById("tasks", "task00000000001")
	if updatedTask1.GetString("title") != "Winning Fresh Title" {
		t.Fatalf("Expected title to be updated to 'Winning Fresh Title', got '%s'", updatedTask1.GetString("title"))
	}

	createdTask2, err := app.FindRecordById("tasks", "task00000000002")
	if err != nil || createdTask2.GetString("title") != "Created While Offline" {
		t.Fatalf("Expected task2 created in database with 'Created While Offline'")
	}
}

func TestRealtime_OfflineSyncAuthorizationAndRules(t *testing.T) {
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

	// Collection with nil ListRule (admin/superuser only)
	secretCol := core.NewBaseCollection("classified_vault")
	secretCol.ListRule = nil
	if err := app.Save(secretCol); err != nil {
		t.Fatalf("Failed to save classified_vault: %v", err)
	}

	// Anonymous request should be rejected with HTTP 403 Forbidden
	reqAnon := httptest.NewRequest(http.MethodGet, "/api/realtime/sync?collection=classified_vault", nil)
	recAnon := httptest.NewRecorder()
	mux.ServeHTTP(recAnon, reqAnon)

	if recAnon.Code != http.StatusForbidden {
		t.Fatalf("Expected HTTP 403 for anonymous sync on secret collection, got %d: %s", recAnon.Code, recAnon.Body.String())
	}
}
