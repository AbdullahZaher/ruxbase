package e2e_test

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

func TestDurableJobs_TransactionalEnqueueRollbackAndCommit(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Rollback test: enqueue inside transaction that returns error
	_ = app.RunInTransaction(func(txApp core.App) error {
		job := &core.Job{
			Id:    "job_rollback_01",
			Queue: "default",
			Task:  "send_receipt",
			Payload: types.JSONMap[any]{
				"orderId": "ord_123",
			},
		}
		if _, err := txApp.Jobs().Enqueue(job); err != nil {
			return err
		}
		return errors.New("simulated business error to trigger rollback")
	})

	// Verify job was NOT inserted
	rolledBackJob, err := app.Jobs().GetJob("job_rollback_01")
	if err == nil || rolledBackJob != nil {
		t.Fatal("Transactional boundary violated! Rolled-back job exists in database")
	}

	// 2. Commit test: enqueue inside transaction that succeeds
	err = app.RunInTransaction(func(txApp core.App) error {
		job := &core.Job{
			Id:    "job_commit_0001",
			Queue: "default",
			Task:  "send_receipt",
			Payload: types.JSONMap[any]{
				"orderId": "ord_456",
			},
		}
		_, err := txApp.Jobs().Enqueue(job)
		return err
	})
	if err != nil {
		t.Fatalf("Failed to commit job transaction: %v", err)
	}

	// Verify job exists and is pending
	committedJob, err := app.Jobs().GetJob("job_commit_0001")
	if err != nil || committedJob == nil {
		t.Fatalf("Committed job was not found in database: %v", err)
	}
	if committedJob.Status != core.JobStatusPending {
		t.Fatalf("Expected status pending, got %s", committedJob.Status)
	}
}

func TestDurableJobs_LeasedWorkerExecutionAndIdempotency(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Idempotency test: enqueue job with unique idempotency key
	job1 := &core.Job{
		Id:             "job_idemp_00001",
		Queue:          "default",
		Task:           "compute_analytics",
		IdempotencyKey: "unique_idemp_key_100",
		Payload: types.JSONMap[any]{
			"metric": "pageviews",
		},
	}
	enqueued1, err := app.Jobs().Enqueue(job1)
	if err != nil {
		t.Fatalf("Failed to enqueue job1: %v", err)
	}

	// Re-enqueue duplicate with same idempotency key
	job2 := &core.Job{
		Id:             "job_idemp_00002",
		Queue:          "default",
		Task:           "compute_analytics",
		IdempotencyKey: "unique_idemp_key_100",
		Payload: types.JSONMap[any]{
			"metric": "pageviews_different",
		},
	}
	enqueued2, err := app.Jobs().Enqueue(job2)
	if err != nil {
		t.Fatalf("Failed to handle duplicate idempotency key: %v", err)
	}

	// Must return the original job without creating duplicate row
	if enqueued2.Id != enqueued1.Id {
		t.Fatalf("Idempotency violation! Expected existing job ID %s, got new ID %s", enqueued1.Id, enqueued2.Id)
	}

	// 2. Worker execution test
	handled := false
	app.Jobs().RegisterHandler("compute_analytics", func(ctx context.Context, job *core.Job) error {
		handled = true
		if job.Payload["metric"] != "pageviews" {
			return errors.New("unexpected payload metric")
		}
		return nil
	})

	processed, err := app.Jobs().ProcessNext(context.Background(), "default", "worker_node_1", 30*time.Second)
	if err != nil {
		t.Fatalf("ProcessNext failed: %v", err)
	}
	if !processed || !handled {
		t.Fatal("Job was not processed by registered handler")
	}

	// Verify job is marked completed
	finishedJob, _ := app.Jobs().GetJob(enqueued1.Id)
	if finishedJob.Status != core.JobStatusCompleted {
		t.Fatalf("Expected job status completed, got %s", finishedJob.Status)
	}
}

func TestDurableJobs_ExponentialBackoffAndDeadLetterQueue(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Enqueue job with max_retries = 2
	job := &core.Job{
		Id:             "job_dlq_test_01",
		Queue:          "default",
		Task:           "flaky_service_call",
		MaxRetries:     2,
		BackoffSeconds: 2,
		Payload: types.JSONMap[any]{
			"target": "external_api",
		},
	}
	if _, err := app.Jobs().Enqueue(job); err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	// Register failing handler
	app.Jobs().RegisterHandler("flaky_service_call", func(ctx context.Context, job *core.Job) error {
		return errors.New("upstream HTTP 503 gateway timeout")
	})

	// Attempt 1: Should fail and back off to pending
	_, err = app.Jobs().ProcessNext(context.Background(), "default", "worker_1", 10*time.Second)
	if err == nil {
		t.Fatal("Expected error from failing task")
	}

	j1, _ := app.Jobs().GetJob("job_dlq_test_01")
	if j1.Status != core.JobStatusFailed {
		t.Fatalf("Expected status failed after attempt 1, got %s", j1.Status)
	}
	if j1.Attempts != 1 {
		t.Fatalf("Expected attempts = 1, got %d", j1.Attempts)
	}

	// Fast-forward run_at for test replay
	_, _ = app.DB().NewQuery("UPDATE {{_jobs}} SET [[run_at]] = now() - INTERVAL '1 second' WHERE [[id]] = 'job_dlq_test_01'").Execute()

	// Attempt 2: Reaches max_retries = 2, must transition to DEAD (DLQ)
	_, err = app.Jobs().ProcessNext(context.Background(), "default", "worker_1", 10*time.Second)
	if err == nil {
		t.Fatal("Expected error from failing task attempt 2")
	}

	j2, _ := app.Jobs().GetJob("job_dlq_test_01")
	if j2.Status != core.JobStatusDead {
		t.Fatalf("Expected job in DEAD letter queue status, got: %s", j2.Status)
	}
	if j2.Attempts != 2 {
		t.Fatalf("Expected attempts = 2, got %d", j2.Attempts)
	}

	// Controlled operator replay: ReplayDeadJob
	if err := app.Jobs().ReplayDeadJob("job_dlq_test_01"); err != nil {
		t.Fatalf("Failed to replay dead job: %v", err)
	}

	jReplayed, _ := app.Jobs().GetJob("job_dlq_test_01")
	if jReplayed.Status != core.JobStatusPending || jReplayed.Attempts != 0 {
		t.Fatalf("Expected replayed job in pending status with 0 attempts, got status=%s attempts=%d", jReplayed.Status, jReplayed.Attempts)
	}
}

func TestDurableJobs_CrashRecoveryAndLeaseReclamation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Enqueue job and claim it with worker A
	job := &core.Job{
		Id:    "job_crash_test1",
		Queue: "default",
		Task:  "batch_report",
	}
	_, _ = app.Jobs().Enqueue(job)

	// Claim with short 1-second lease
	claimed, err := app.Jobs().ClaimNext("default", "worker_node_crash_A", 1*time.Second)
	if err != nil || claimed == nil {
		t.Fatalf("Failed to claim job: %v", err)
	}
	if claimed.Status != core.JobStatusRunning {
		t.Fatalf("Expected status running, got %s", claimed.Status)
	}

	// Worker A crashes (stops responding). Expire lease in database:
	_, _ = app.DB().NewQuery("UPDATE {{_jobs}} SET [[leased_until]] = now() - INTERVAL '5 seconds' WHERE [[id]] = 'job_crash_test1'").Execute()

	// Reclaim stale leases
	reclaimedCount, err := app.Jobs().ReclaimStaleLeases()
	if err != nil {
		t.Fatalf("ReclaimStaleLeases failed: %v", err)
	}
	if reclaimedCount != 1 {
		t.Fatalf("Expected 1 reclaimed stale lease, got %d", reclaimedCount)
	}

	// Worker B can now claim the reclaimed job
	claimedByB, err := app.Jobs().ClaimNext("default", "worker_node_survivor_B", 30*time.Second)
	if err != nil || claimedByB == nil {
		t.Fatalf("Survivor worker B failed to claim reclaimed job: %v", err)
	}
	if claimedByB.Id != "job_crash_test1" || claimedByB.LeasedBy != "worker_node_survivor_B" {
		t.Fatalf("Unexpected claimed job state: %+v", claimedByB)
	}
}

func TestDurableJobs_SignedWebhookDeliveryWithHMAC(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	secret := "test_webhook_hmac_secret_key_123"
	var receivedHeaderSig string
	var receivedBody []byte

	// Mock webhook receiving server
	webhookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaderSig = r.Header.Get("X-Ruxbase-Signature")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received": true}`))
	}))
	defer webhookServer.Close()

	// 1. Register webhook
	webhook, err := app.Jobs().RegisterWebhook("order_notifications", webhookServer.URL, secret, []string{"orders.*"})
	if err != nil {
		t.Fatalf("Failed to register webhook: %v", err)
	}

	// 2. Publish outbox event
	outboxEvt, err := app.Jobs().PublishOutbox("orders.created", "orders", map[string]any{
		"orderId": "ord_9999",
		"total":   150.00,
	})
	if err != nil {
		t.Fatalf("Failed to publish outbox event: %v", err)
	}

	// 3. Deliver webhook
	attempt, err := app.Jobs().DeliverWebhook(context.Background(), webhook, outboxEvt)
	if err != nil {
		t.Fatalf("DeliverWebhook failed: %v", err)
	}

	if attempt.StatusCode != http.StatusOK {
		t.Fatalf("Expected status code 200, got %d", attempt.StatusCode)
	}

	// 4. Verify HMAC-SHA256 signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(receivedBody)
	expectedSig := hex.EncodeToString(mac.Sum(nil))

	if receivedHeaderSig != expectedSig {
		t.Fatalf("HMAC signature mismatch! Expected %s, got %s", expectedSig, receivedHeaderSig)
	}

	// Verify attempt logged in database
	var attemptCount int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_webhook_attempts}} WHERE [[webhook_id]] = {:wId}").
		Bind(dbx.Params{"wId": webhook.Id}).Row(&attemptCount)

	if attemptCount != 1 {
		t.Fatalf("Expected 1 recorded webhook attempt in database, got %d", attemptCount)
	}
}
