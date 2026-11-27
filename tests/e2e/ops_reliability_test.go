package e2e_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestOps_ObservabilityMetricsCapacityAndAlertsSecurity(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Seed a dead job to confirm dlq_non_empty alert triggers properly
	_, err = app.Jobs().Enqueue(&core.Job{
		Id:         "job_dead_alert_test",
		Queue:      "default",
		Task:       "failing_sync",
		MaxRetries: 1,
	})
	if err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}
	_, _ = app.Jobs().ProcessNext(context.Background(), "default", "worker_ops", 5*time.Second)

	// Obtain normal user & superuser auth tokens
	user, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find test user: %v", err)
	}
	userToken, err := user.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to create user token: %v", err)
	}

	superuser, err := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find superuser: %v", err)
	}
	superToken, err := superuser.NewAuthToken()
	if err != nil {
		t.Fatalf("Failed to create superuser token: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to build API router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// 1. Unauthenticated caller and regular authenticated user MUST be denied on all /api/ops/* endpoints
	opsPaths := []string{"/api/ops/metrics", "/api/ops/capacity", "/api/ops/alerts"}
	for _, p := range opsPaths {
		// Guest
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized && rec.Code != http.StatusForbidden {
			t.Fatalf("Security breach on %s for guest! Expected 401/403, got %d", p, rec.Code)
		}

		// Regular user
		reqUser := httptest.NewRequest(http.MethodGet, p, nil)
		reqUser.Header.Set("Authorization", userToken)
		recUser := httptest.NewRecorder()
		mux.ServeHTTP(recUser, reqUser)
		if recUser.Code != http.StatusUnauthorized && recUser.Code != http.StatusForbidden {
			t.Fatalf("Security breach on %s for regular user! Expected 401/403, got %d", p, recUser.Code)
		}
	}

	// 2. Superuser can retrieve JSON metrics
	reqMetrics := httptest.NewRequest(http.MethodGet, "/api/ops/metrics", nil)
	reqMetrics.Header.Set("Authorization", superToken)
	recMetrics := httptest.NewRecorder()
	mux.ServeHTTP(recMetrics, reqMetrics)
	if recMetrics.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /api/ops/metrics, got %d: %s", recMetrics.Code, recMetrics.Body.String())
	}

	var snap core.OpsMetricsSnapshot
	if err := json.Unmarshal(recMetrics.Body.Bytes(), &snap); err != nil {
		t.Fatalf("Failed to unmarshal metrics snapshot: %v", err)
	}
	if snap.Runtime.Goroutines <= 0 || snap.Runtime.HeapAllocBytes == 0 || snap.Runtime.LiveHeapBytes == 0 || snap.Runtime.HeapInuseBytes == 0 || snap.Database.SizeBytes <= 0 || snap.Jobs.Dead != 1 {
		t.Fatalf("Unexpected metrics snapshot values: %+v", snap)
	}

	// 3. Superuser can retrieve Prometheus exposition metrics
	reqProm := httptest.NewRequest(http.MethodGet, "/api/ops/metrics?format=prometheus", nil)
	reqProm.Header.Set("Authorization", superToken)
	recProm := httptest.NewRecorder()
	mux.ServeHTTP(recProm, reqProm)
	if recProm.Code != http.StatusOK {
		t.Fatalf("Expected 200 on prometheus metrics, got %d", recProm.Code)
	}
	promBody := recProm.Body.String()
	if !strings.Contains(promBody, "ruxbase_jobs_dead 1") || !strings.Contains(promBody, "ruxbase_db_size_bytes") || !strings.Contains(promBody, "ruxbase_runtime_live_heap_bytes") {
		t.Fatalf("Prometheus output missing expected metrics:\n%s", promBody)
	}

	// 4. Superuser can evaluate connection capacity budget
	reqCap := httptest.NewRequest(http.MethodGet, "/api/ops/capacity", nil)
	reqCap.Header.Set("Authorization", superToken)
	recCap := httptest.NewRecorder()
	mux.ServeHTTP(recCap, reqCap)
	if recCap.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /api/ops/capacity, got %d", recCap.Code)
	}
	var budget core.CapacityBudgetReport
	if err := json.Unmarshal(recCap.Body.Bytes(), &budget); err != nil {
		t.Fatalf("Failed to unmarshal capacity report: %v", err)
	}
	if budget.ServerMaxConnections <= 0 || budget.TotalPerNodeMax <= 0 || budget.MeasuredHeapBytes == 0 {
		t.Fatalf("Unexpected capacity budget values: %+v", budget)
	}

	// 5. Superuser sees active dlq_non_empty alert
	reqAlerts := httptest.NewRequest(http.MethodGet, "/api/ops/alerts", nil)
	reqAlerts.Header.Set("Authorization", superToken)
	recAlerts := httptest.NewRecorder()
	mux.ServeHTTP(recAlerts, reqAlerts)
	if recAlerts.Code != http.StatusOK {
		t.Fatalf("Expected 200 on /api/ops/alerts, got %d", recAlerts.Code)
	}
	var alertResp struct {
		Count  int             `json:"count"`
		Alerts []core.OpsAlert `json:"alerts"`
	}
	if err := json.Unmarshal(recAlerts.Body.Bytes(), &alertResp); err != nil {
		t.Fatalf("Failed to unmarshal alerts response: %v", err)
	}
	foundDLQAlert := false
	for _, a := range alertResp.Alerts {
		if a.Code == "dlq_non_empty" && a.Severity == "warning" {
			foundDLQAlert = true
		}
	}
	if !foundDLQAlert {
		t.Fatalf("Expected dlq_non_empty warning alert, got: %+v", alertResp.Alerts)
	}
}

func TestOps_ReleaseBlockersAndStorageCorruptionDetection(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Create valid bucket and object
	bucket, err := app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_integrity_ops",
		Name:   "critical_backups",
		Public: false,
	})
	if err != nil {
		t.Fatalf("Failed to create bucket: %v", err)
	}

	_, err = app.Storage().UploadObject(
		context.Background(),
		"critical_backups",
		"config.json",
		"admin",
		"application/json",
		[]byte(`{"version":"1.0.0"}`),
		nil,
	)
	if err != nil {
		t.Fatalf("Failed to upload object: %v", err)
	}

	// 1. Clean system must pass all release blockers
	cleanReport, err := core.CheckReleaseBlockers(context.Background(), app)
	if err != nil {
		t.Fatalf("CheckReleaseBlockers failed: %v", err)
	}
	if !cleanReport.Passed {
		t.Fatalf("Expected clean report to pass, got findings: %+v", cleanReport.Findings)
	}

	// 2. Corrupt underlying file in storage filesystem (simulate bitrot/tampering)
	fsys, err := app.NewFilesystem()
	if err != nil {
		t.Fatalf("Failed to open filesystem: %v", err)
	}
	objKey := path.Join("_storage", bucket.Id, "config.json")
	if err := fsys.Upload([]byte(`{"corrupted":true}`), objKey); err != nil {
		fsys.Close()
		t.Fatalf("Failed to write corrupted payload: %v", err)
	}
	fsys.Close()

	// 3. Release blocker gate MUST now fail with storage_integrity_violation
	corruptReport, err := core.CheckReleaseBlockers(context.Background(), app)
	if err != nil {
		t.Fatalf("CheckReleaseBlockers error: %v", err)
	}
	if corruptReport.Passed {
		t.Fatal("Release blocker check wrongly passed despite corrupted storage blob checksum!")
	}
	detectedCorruption := false
	for _, f := range corruptReport.Findings {
		if f.Code == "storage_integrity_violation" && f.Blocked {
			detectedCorruption = true
		}
	}
	if !detectedCorruption {
		t.Fatalf("Expected storage_integrity_violation blocker finding, got: %+v", corruptReport.Findings)
	}
}

func TestOps_PrivateSelfHostedInstallerDryRun(t *testing.T) {
	tmpDir := t.TempDir()

	// Locate repo root install script relative to tests/e2e
	scriptPath := filepath.Join("..", "..", "scripts", "install-selfhosted.sh")
	cmd := exec.Command("bash", scriptPath, "--dir", tmpDir, "--dry-run")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Installer dry-run failed: %v\nOutput: %s", err, string(out))
	}

	envPath := filepath.Join(tmpDir, ".env")
	info, err := os.Stat(envPath)
	if err != nil {
		t.Fatalf("Expected .env file created at %s: %v", envPath, err)
	}

	// Must have restrictive 0600 permissions
	if info.Mode().Perm() != 0600 {
		t.Fatalf("Security violation: expected .env mode 0600, got %o", info.Mode().Perm())
	}

	envRaw, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("Failed to read .env: %v", err)
	}
	envContent := string(envRaw)

	// Verify loopback binding and non-empty private secrets
	if !strings.Contains(envContent, "RUXBASE_HTTP_ADDR=127.0.0.1:8090") ||
		!strings.Contains(envContent, "POSTGRES_PASSWORD=") ||
		!strings.Contains(envContent, "APP_PASSWORD=") ||
		!strings.Contains(envContent, "RUXBASE_ENCRYPTION_KEY=") {
		t.Fatalf("Generated .env missing required private configuration fields:\n%s", envContent)
	}

	// Extract secret values and verify none were leaked in stdout
	for _, line := range strings.Split(envContent, "\n") {
		if strings.HasPrefix(line, "POSTGRES_PASSWORD=") || strings.HasPrefix(line, "RUXBASE_ENCRYPTION_KEY=") {
			_, val, _ := strings.Cut(line, "=")
			if len(val) < 32 {
				t.Fatalf("Generated secret %q too weak (< 32 chars)", line)
			}
			if strings.Contains(string(out), val) {
				t.Fatalf("Security violation: installer printed secret value to stdout!")
			}
		}
	}

	// Second run must preserve existing .env credentials idempotently
	cmd2 := exec.Command("bash", scriptPath, "--dir", tmpDir, "--dry-run")
	out2, err := cmd2.CombinedOutput()
	if err != nil {
		t.Fatalf("Second installer run failed: %v", err)
	}
	if !strings.Contains(string(out2), "Existing environment file preserved") {
		t.Fatalf("Expected installer to preserve existing .env, got:\n%s", string(out2))
	}
	envRawAfter, _ := os.ReadFile(envPath)
	if string(envRawAfter) != envContent {
		t.Fatal("Installer overwrote existing .env secrets on second invocation!")
	}
}
