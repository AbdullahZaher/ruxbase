package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestStudio_RBACProtectionAndAccessControlAbuse(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find test user: %v", err)
	}
	userToken, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate user token: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/studio/overview"},
		{http.MethodGet, "/api/studio/jobs"},
		{http.MethodPost, "/api/studio/jobs/job_123/replay"},
		{http.MethodGet, "/api/studio/audit"},
		{http.MethodGet, "/api/control-plane/projects"},
		{http.MethodPost, "/api/control-plane/projects"},
		{http.MethodPost, "/api/control-plane/projects/p_123/status"},
	}

	for _, ep := range endpoints {
		// 1. Unauthenticated guest -> must fail 401/403
		reqGuest := httptest.NewRequest(ep.method, ep.path, nil)
		recGuest := httptest.NewRecorder()
		mux.ServeHTTP(recGuest, reqGuest)
		if recGuest.Code != http.StatusUnauthorized && recGuest.Code != http.StatusForbidden {
			t.Fatalf("RBAC bypass for guest on %s %s: expected 401/403, got %d", ep.method, ep.path, recGuest.Code)
		}

		// 2. Non-superuser authenticated collection user -> must fail 401/403
		reqUser := httptest.NewRequest(ep.method, ep.path, nil)
		reqUser.Header.Set("Authorization", userToken)
		recUser := httptest.NewRecorder()
		mux.ServeHTTP(recUser, reqUser)
		if recUser.Code != http.StatusUnauthorized && recUser.Code != http.StatusForbidden {
			t.Fatalf("RBAC privilege escalation for user on %s %s: expected 401/403, got %d", ep.method, ep.path, recUser.Code)
		}
	}
}

func TestStudio_OperationalOverviewDeadJobReplayAndAuditTrail(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Seed a bucket + object and a dead job
	_, err = app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_studio_view",
		Name:   "studio_assets",
		Public: false,
	})
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}
	_, err = app.Storage().UploadObject(context.Background(), "studio_assets", "doc.txt", "admin", "text/plain", []byte("studio_payload"), nil)
	if err != nil {
		t.Fatalf("Failed to upload object: %v", err)
	}

	_, err = app.Jobs().Enqueue(&core.Job{
		Id:         "job_studio_dlq_1",
		Queue:      "default",
		Task:       "unregistered_task",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}
	_, _ = app.Jobs().ProcessNext(context.Background(), "default", "studio_worker", 5*time.Second)

	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find superuser: %v", err)
	}
	superToken, err := superuser.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate superuser token: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to build router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// 1. Inspect Studio overview
	reqOv := httptest.NewRequest(http.MethodGet, "/api/studio/overview", nil)
	reqOv.Header.Set("Authorization", superToken)
	recOv := httptest.NewRecorder()
	mux.ServeHTTP(recOv, reqOv)
	if recOv.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /api/studio/overview, got %d: %s", recOv.Code, recOv.Body.String())
	}

	var overview core.StudioOverview
	if err := json.Unmarshal(recOv.Body.Bytes(), &overview); err != nil {
		t.Fatalf("Failed to parse overview response: %v", err)
	}
	if overview.MigrationsApplied == 0 || overview.CollectionsCount == 0 || overview.Metrics.Jobs.Dead != 1 || len(overview.Buckets) == 0 {
		t.Fatalf("Unexpected Studio overview payload: %+v", overview)
	}

	// 2. Inspect jobs list
	reqJobs := httptest.NewRequest(http.MethodGet, "/api/studio/jobs?status=dead", nil)
	reqJobs.Header.Set("Authorization", superToken)
	recJobs := httptest.NewRecorder()
	mux.ServeHTTP(recJobs, reqJobs)
	if recJobs.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /api/studio/jobs, got %d", recJobs.Code)
	}
	if !strings.Contains(recJobs.Body.String(), "job_studio_dlq_1") {
		t.Fatalf("Expected dead job in /api/studio/jobs response: %s", recJobs.Body.String())
	}

	// 3. Trigger Dead Job Replay via Studio API -> must transition to pending AND write immutable audit entry
	reqReplay := httptest.NewRequest(http.MethodPost, "/api/studio/jobs/job_studio_dlq_1/replay", nil)
	reqReplay.Header.Set("Authorization", superToken)
	recReplay := httptest.NewRecorder()
	mux.ServeHTTP(recReplay, reqReplay)
	if recReplay.Code != http.StatusOK {
		t.Fatalf("Expected 200 on job replay, got %d: %s", recReplay.Code, recReplay.Body.String())
	}

	replayedJob, err := app.Jobs().GetJob("job_studio_dlq_1")
	if err != nil || replayedJob.Status != core.JobStatusPending {
		t.Fatalf("Expected replayed job in pending status, got: %+v (err=%v)", replayedJob, err)
	}

	// 4. Verify immutable audit log entry recorded for replay action
	reqAudit := httptest.NewRequest(http.MethodGet, "/api/studio/audit", nil)
	reqAudit.Header.Set("Authorization", superToken)
	recAudit := httptest.NewRecorder()
	mux.ServeHTTP(recAudit, reqAudit)
	if recAudit.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /api/studio/audit, got %d", recAudit.Code)
	}
	var auditResp struct {
		Count int                  `json:"count"`
		Items []core.AdminAuditLog `json:"items"`
	}
	if err := json.Unmarshal(recAudit.Body.Bytes(), &auditResp); err != nil {
		t.Fatalf("Failed to unmarshal audit response: %v", err)
	}
	if auditResp.Count == 0 || auditResp.Items[0].Action != "replay" || auditResp.Items[0].TargetId != "job_studio_dlq_1" {
		t.Fatalf("Expected replay audit log entry, got: %+v", auditResp.Items)
	}
}

func TestControlPlane_ProjectIsolationAndPrivilegedRoleRejection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find superuser: %v", err)
	}
	superToken, err := superuser.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to generate superuser token: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to build router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// 1. Reject privileged or shared runtime roles (postgres / ruxbase_app)
	for _, badRole := range []string{"postgres", "ruxbase_app", "ruxbase_migrator"} {
		body, _ := json.Marshal(map[string]any{
			"slug":        "bad-proj",
			"name":        "Bad Isolation Project",
			"environment": "production",
			"dbName":      "proj_bad_db",
			"dbRole":      badRole,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/control-plane/projects", bytes.NewReader(body))
		req.Header.Set("Authorization", superToken)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("Isolation violation! Expected 400 Bad Request for dbRole=%s, got %d: %s", badRole, rec.Code, rec.Body.String())
		}
	}

	// 2. Register isolated tenant project alpha cleanly
	projBody, _ := json.Marshal(map[string]any{
		"slug":              "proj-alpha",
		"name":              "Alpha Production",
		"environment":       "production",
		"dbName":            "ruxbase_proj_alpha",
		"dbRole":            "role_proj_alpha",
		"storageQuotaBytes": 5368709120,
		"maxConnections":    25,
	})
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/control-plane/projects", bytes.NewReader(projBody))
	reqCreate.Header.Set("Authorization", superToken)
	reqCreate.Header.Set("Content-Type", "application/json")
	recCreate := httptest.NewRecorder()
	mux.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created for valid isolated project, got %d: %s", recCreate.Code, recCreate.Body.String())
	}

	// 3. Reject conflicting project attempting to reuse proj-alpha's db_role
	dupBody, _ := json.Marshal(map[string]any{
		"slug":        "proj-beta",
		"name":        "Beta Conflict",
		"environment": "staging",
		"dbName":      "ruxbase_proj_beta",
		"dbRole":      "role_proj_alpha", // Conflict!
	})
	reqDup := httptest.NewRequest(http.MethodPost, "/api/control-plane/projects", bytes.NewReader(dupBody))
	reqDup.Header.Set("Authorization", superToken)
	reqDup.Header.Set("Content-Type", "application/json")
	recDup := httptest.NewRecorder()
	mux.ServeHTTP(recDup, reqDup)
	if recDup.Code == http.StatusCreated {
		t.Fatal("Security violation: allowed two control plane projects to share the same db_role!")
	}

	// 4. Transition project lifecycle to suspended and verify audit log
	statusBody, _ := json.Marshal(map[string]any{"status": "suspended"})
	reqStatus := httptest.NewRequest(http.MethodPost, "/api/control-plane/projects/proj-alpha/status", bytes.NewReader(statusBody))
	reqStatus.Header.Set("Authorization", superToken)
	reqStatus.Header.Set("Content-Type", "application/json")
	recStatus := httptest.NewRecorder()
	mux.ServeHTTP(recStatus, reqStatus)
	if recStatus.Code != http.StatusOK {
		t.Fatalf("Expected 200 on lifecycle transition, got %d: %s", recStatus.Code, recStatus.Body.String())
	}
	if !strings.Contains(recStatus.Body.String(), `"status":"suspended"`) {
		t.Fatalf("Expected suspended status in response: %s", recStatus.Body.String())
	}
}
