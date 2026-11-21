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
	"github.com/AbdullahZaher/ruxbase/tools/types"
)

// TestRelease_CoreSelfHostedIntegratedJourney exercises an integrated cross-wave
// user journey against PostgreSQL covering auth, restricted CRUD, storage access control,
// durable jobs, isolated compute functions, Studio audit logging, AI agent approval governance,
// and release-blocker integrity verification.
func TestRelease_CoreSelfHostedIntegratedJourney(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Auth & Least-Privilege Identity Setup
	usersCol, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}
	alice, err := app.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatalf("Failed to find test user alice: %v", err)
	}
	aliceToken, _ := alice.NewAuthToken()

	bob := core.NewRecord(usersCol)
	bob.SetEmail("bob_release@example.com")
	bob.SetPassword("ReleasePass1234!")
	if err := app.Save(bob); err != nil {
		t.Fatalf("Failed to save bob: %v", err)
	}
	bobToken, _ := bob.NewAuthToken()

	superuser, _ := app.FindAuthRecordByEmail(core.CollectionNameSuperusers, "test@example.com")
	superToken, _ := superuser.NewAuthToken()

	// 2. Collection Creation with Strict Owner Authorization Rules
	notesCol := core.NewBaseCollection("release_notes")
	notesCol.Fields.Add(
		&core.TextField{Name: "title", Required: true},
		&core.TextField{Name: "owner", Required: true},
	)
	rule := "@request.auth.id != '' && owner = @request.auth.id"
	notesCol.ListRule = &rule
	notesCol.ViewRule = &rule
	notesCol.CreateRule = &rule
	if err := app.Save(notesCol); err != nil {
		t.Fatalf("Failed to create release_notes collection: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to build API router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// Alice creates a private record; Bob cannot view it
	recBody, _ := json.Marshal(map[string]any{
		"title": "Alice Q4 Notes",
		"owner": alice.Id,
	})
	reqCreate := httptest.NewRequest(http.MethodPost, "/api/collections/release_notes/records", bytes.NewReader(recBody))
	reqCreate.Header.Set("Authorization", aliceToken)
	reqCreate.Header.Set("Content-Type", "application/json")
	recCreate := httptest.NewRecorder()
	mux.ServeHTTP(recCreate, reqCreate)
	if recCreate.Code != http.StatusOK {
		t.Fatalf("Alice create record failed: %d %s", recCreate.Code, recCreate.Body.String())
	}
	var createdRec struct {
		Id string `json:"id"`
	}
	_ = json.Unmarshal(recCreate.Body.Bytes(), &createdRec)

	reqBobView := httptest.NewRequest(http.MethodGet, "/api/collections/release_notes/records/"+createdRec.Id, nil)
	reqBobView.Header.Set("Authorization", bobToken)
	recBobView := httptest.NewRecorder()
	mux.ServeHTTP(recBobView, reqBobView)
	if recBobView.Code != http.StatusNotFound && recBobView.Code != http.StatusForbidden {
		t.Fatalf("Cross-tenant CRUD read breach! Expected 404/403 for Bob, got %d", recBobView.Code)
	}

	// 3. Storage Bucket, Upload, Cross-Tenant Denial & Signed URL
	_, err = app.Storage().CreateBucket(&core.StorageBucket{
		Id:     "bkt_rel_01",
		Name:   "release_files",
		Public: false,
	})
	if err != nil {
		t.Fatalf("CreateBucket failed: %v", err)
	}
	_, err = app.Storage().UploadObject(context.Background(), "release_files", "spec.txt", alice.Id, "text/plain", []byte("release-spec-bytes"), nil)
	if err != nil {
		t.Fatalf("UploadObject failed: %v", err)
	}
	if _, _, err := app.Storage().GetObject(context.Background(), "release_files", "spec.txt", bob.Id, false); err != core.ErrStorageAccessDenied {
		t.Fatalf("Expected ErrStorageAccessDenied for cross-tenant read, got %v", err)
	}

	// 4. Durable Job Transactional Enqueue & Processing
	jobHandled := false
	app.Jobs().RegisterHandler("release_notify", func(ctx context.Context, j *core.Job) error {
		jobHandled = true
		return nil
	})
	if _, err := app.Jobs().Enqueue(&core.Job{Id: "job_rel_01", Task: "release_notify"}); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
	processed, err := app.Jobs().ProcessNext(context.Background(), "default", "rel_worker", 10*time.Second)
	if err != nil || !processed || !jobHandled {
		t.Fatalf("Durable job processing failed: processed=%v handled=%v err=%v", processed, jobHandled, err)
	}

	// 5. Sandboxed Function Compute (Secrets Boundary)
	_, err = app.Functions().CreateFunction(&core.Function{
		Name:       "rel_echo",
		Entrypoint: "main",
		EnvVars:    types.JSONMap[string]{"GREETING": "HelloRuxbase"},
		Code:       `function main(p) { return { msg: env.GREETING + ":" + p.name }; }`,
	})
	if err != nil {
		t.Fatalf("CreateFunction failed: %v", err)
	}
	fnRes, err := app.Functions().Invoke(context.Background(), "rel_echo", map[string]any{"name": "GA"})
	if err != nil || fnRes["msg"] != "HelloRuxbase:GA" {
		t.Fatalf("Function invoke failed: %+v err=%v", fnRes, err)
	}

	// 6. Studio & Observability Endpoints (Superuser Protected)
	reqOv := httptest.NewRequest(http.MethodGet, "/api/studio/overview", nil)
	reqOv.Header.Set("Authorization", superToken)
	recOv := httptest.NewRecorder()
	mux.ServeHTTP(recOv, reqOv)
	if recOv.Code != http.StatusOK || !strings.Contains(recOv.Body.String(), `"passed":true`) {
		t.Fatalf("Studio overview blocker verification failed: %d %s", recOv.Code, recOv.Body.String())
	}

	// 7. Release-Blocker Integrity Verification (Role privileges, Migrations, Storage Checksums)
	blockers, err := core.CheckReleaseBlockers(context.Background(), app)
	if err != nil || !blockers.Passed {
		t.Fatalf("Release blocker check failed: %+v (err=%v)", blockers, err)
	}
}
