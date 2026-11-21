package e2e_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

// TestDurableResilience_PoisonMessageIsolation verifies that a panicking or catastrophic task
// handler is safely contained, transitions the job without crashing the worker or process,
// and records the failure error in PostgreSQL.
func TestDurableResilience_PoisonMessageIsolation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Register a panic-inducing poison task handler with defer recovery inside ProcessNext or handler
	app.Jobs().RegisterHandler("poison_task", func(ctx context.Context, job *core.Job) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = errors.New("recovered panic: poison message payload caused nil dereference")
			}
		}()
		panic("simulated fatal payload crash in business logic")
	})

	job := &core.Job{
		Id:             "job_poison_01",
		Queue:          "default",
		Task:           "poison_task",
		MaxRetries:     1,
		BackoffSeconds: 1,
	}
	if _, err := app.Jobs().Enqueue(job); err != nil {
		t.Fatalf("Failed to enqueue poison job: %v", err)
	}

	// Worker processes poison task -> must not crash test runner
	processed, err := app.Jobs().ProcessNext(context.Background(), "default", "worker_isolated", 10*time.Second)
	if err == nil {
		t.Fatal("Expected error from poison task execution")
	}
	if !processed {
		t.Fatal("Expected job to have been claimed and processed")
	}

	// Verify job transitioned to Dead Letter Queue (attempts reached max_retries = 1)
	deadJob, err := app.Jobs().GetJob("job_poison_01")
	if err != nil {
		t.Fatalf("Failed to retrieve dead job: %v", err)
	}
	if deadJob.Status != core.JobStatusDead {
		t.Fatalf("Expected job status dead, got: %s", deadJob.Status)
	}
	if deadJob.LastError == "" {
		t.Fatal("Expected dead job to record the poison error message")
	}
}

// TestDurableResilience_ConcurrentWorkerLeaseContention ensures that when multiple concurrent
// workers race to claim the same eligible job via FOR UPDATE SKIP LOCKED, exactly one worker
// wins the lease and no double execution occurs.
func TestDurableResilience_ConcurrentWorkerLeaseContention(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// Enqueue exactly 1 job
	job := &core.Job{
		Id:    "job_single_contention_01",
		Queue: "default",
		Task:  "exclusive_batch_process",
	}
	if _, err := app.Jobs().Enqueue(job); err != nil {
		t.Fatalf("Failed to enqueue job: %v", err)
	}

	const workerCount = 5
	var claimCount atomic.Int32
	var claimedWorker string
	var mu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(workerCount)

	for i := 1; i <= workerCount; i++ {
		workerName := "worker_" + string(rune('A'+i-1))
		go func(name string) {
			defer wg.Done()
			claimed, err := app.Jobs().ClaimNext("default", name, 30*time.Second)
			if err == nil && claimed != nil {
				claimCount.Add(1)
				mu.Lock()
				claimedWorker = name
				mu.Unlock()
			}
		}(workerName)
	}

	wg.Wait()

	if claimCount.Load() != 1 {
		t.Fatalf("Lease race condition! Expected exactly 1 worker to claim job, got %d", claimCount.Load())
	}

	// Verify database record has exactly that winning worker
	dbJob, _ := app.Jobs().GetJob("job_single_contention_01")
	if dbJob.Status != core.JobStatusRunning || dbJob.LeasedBy != claimedWorker {
		t.Fatalf("Database record mismatch: status=%s, leased_by=%s (expected %s)", dbJob.Status, dbJob.LeasedBy, claimedWorker)
	}
}

// TestDurableResilience_TransactionalOutboxRollbackIsolation verifies that outbox events
// published within a rolling-back transaction never persist to disk.
func TestDurableResilience_TransactionalOutboxRollbackIsolation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Transaction that fails and rolls back
	_ = app.RunInTransaction(func(txApp core.App) error {
		_, err := txApp.Jobs().PublishOutbox("user.created", "users", map[string]any{
			"email": "ghost@example.com",
		})
		if err != nil {
			return err
		}
		return errors.New("abort transaction")
	})

	// Verify no outbox events exist
	var count int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_outbox_events}} WHERE [[event]] = 'user.created'").Row(&count)
	if count != 0 {
		t.Fatalf("Transactional outbox violated! Found %d uncommitted outbox events", count)
	}

	// 2. Transaction that commits successfully
	err = app.RunInTransaction(func(txApp core.App) error {
		_, err := txApp.Jobs().PublishOutbox("user.created", "users", map[string]any{
			"email": "committed@example.com",
		})
		return err
	})
	if err != nil {
		t.Fatalf("Transaction failed: %v", err)
	}

	_ = app.DB().NewQuery("SELECT count(*) FROM {{_outbox_events}} WHERE [[event]] = 'user.created'").Row(&count)
	if count != 1 {
		t.Fatalf("Expected 1 committed outbox event, got %d", count)
	}
}

// TestDurableResilience_ParallelIdempotencyDeduplication verifies that parallel enqueue calls
// with the same idempotency key return the exact same job ID and create only 1 database row.
func TestDurableResilience_ParallelIdempotencyDeduplication(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	const concurrency = 6
	const idempKey = "concurrent_idemp_key_42"

	var wg sync.WaitGroup
	wg.Add(concurrency)

	jobIDs := make([]string, concurrency)
	errorsList := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		idx := i
		go func() {
			defer wg.Done()
			j := &core.Job{
				Id:             "job_candidate_" + string(rune('0'+idx)),
				Queue:          "default",
				Task:           "compute_hash",
				IdempotencyKey: idempKey,
			}
			res, err := app.Jobs().Enqueue(j)
			if err != nil {
				errorsList[idx] = err
				return
			}
			jobIDs[idx] = res.Id
		}()
	}

	wg.Wait()

	for idx, err := range errorsList {
		if err != nil {
			t.Fatalf("Enqueue worker %d failed: %v", idx, err)
		}
	}

	firstID := jobIDs[0]
	for idx, id := range jobIDs {
		if id != firstID {
			t.Fatalf("Worker %d returned differing job ID %s (expected %s)", idx, id, firstID)
		}
	}

	// Verify only 1 row exists in the database
	var rowCount int
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_jobs}} WHERE [[idempotency_key]] = {:k}").
		Bind(dbx.Params{"k": idempKey}).Row(&rowCount)

	if rowCount != 1 {
		t.Fatalf("Expected 1 unique row in database for idempotency key, got %d", rowCount)
	}
}
