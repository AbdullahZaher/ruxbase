package core_test

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestJobsManager_OutboxConcurrency(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	const totalEvents = 60
	topic := fmt.Sprintf("test_topic_%d", time.Now().UnixNano())

	for i := 0; i < totalEvents; i++ {
		_, err = jm.PublishOutbox("order.created", topic, map[string]any{"seq": i})
		if err != nil {
			t.Fatalf("PublishOutbox failed: %v", err)
		}
	}

	var publishedCalls atomic.Int64
	var duplicateDeliveries atomic.Int64
	var seenEvents sync.Map
	var wg sync.WaitGroup

	const numWorkers = 8
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerId int) {
			defer wg.Done()
			for {
				n, err := jm.ProcessPendingOutbox(10, func(evt *core.OutboxEvent) error {
					// Simulate 5ms downstream delivery latency
					time.Sleep(5 * time.Millisecond)

					publishedCalls.Add(1)
					val, _ := seenEvents.LoadOrStore(evt.Id, &atomic.Int64{})
					if val.(*atomic.Int64).Add(1) > 1 {
						duplicateDeliveries.Add(1)
					}
					return nil
				})
				if err != nil {
					t.Errorf("ProcessPendingOutbox error on worker %d: %v", workerId, err)
					return
				}
				if n == 0 {
					return // No more pending events
				}
			}
		}(w)
	}

	wg.Wait()

	if duplicateDeliveries.Load() != 0 {
		t.Fatalf("expected 0 duplicate deliveries, got %d (concurrency race!)", duplicateDeliveries.Load())
	}

	if publishedCalls.Load() != totalEvents {
		t.Fatalf("expected %d published calls, got %d", totalEvents, publishedCalls.Load())
	}

	// Verify all events in DB are status = 'published'
	var publishedInDB int64
	_ = app.DB().NewQuery("SELECT count(*) FROM {{_outbox_events}} WHERE [[status]] = 'published'").Row(&publishedInDB)
	if publishedInDB != totalEvents {
		t.Fatalf("expected %d published events in DB, got %d", totalEvents, publishedInDB)
	}
}

func TestJobsManager_OutboxRetryAndFailure(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("user.signup", "user_topic", map[string]any{"user": "alice"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	// 1. First attempt fails
	simulatedErr := errors.New("simulated webhook endpoint timeout")
	n, err := jm.ProcessPendingOutbox(10, func(e *core.OutboxEvent) error {
		return simulatedErr
	})
	if err != nil {
		t.Fatalf("ProcessPendingOutbox error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 successfully published, got %d", n)
	}

	// Check DB: status should remain 'pending' with attempts = 1 and last_error recorded
	var status, lastErr string
	var attempts int
	err = app.DB().NewQuery("SELECT [[status]], [[attempts]], [[last_error]] FROM {{_outbox_events}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": evt.Id}).
		Row(&status, &attempts, &lastErr)
	if err != nil {
		t.Fatalf("failed to query outbox event: %v", err)
	}

	if status != "pending" {
		t.Fatalf("expected status 'pending' after 1 retryable failure, got %q", status)
	}
	if attempts != 1 {
		t.Fatalf("expected attempts 1, got %d", attempts)
	}
	if lastErr != simulatedErr.Error() {
		t.Fatalf("expected last_error %q, got %q", simulatedErr.Error(), lastErr)
	}

	// 2. Second attempt succeeds
	n, err = jm.ProcessPendingOutbox(10, func(e *core.OutboxEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("ProcessPendingOutbox second attempt error: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 successfully published, got %d", n)
	}

	_ = app.DB().NewQuery("SELECT [[status]], [[attempts]], [[last_error]] FROM {{_outbox_events}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": evt.Id}).
		Row(&status, &attempts, &lastErr)

	if status != "published" {
		t.Fatalf("expected status 'published', got %q", status)
	}
	if attempts != 2 {
		t.Fatalf("expected attempts 2, got %d", attempts)
	}
	if lastErr != "" {
		t.Fatalf("expected cleared last_error on publish, got %q", lastErr)
	}
}

// 1. Multiple workers simultaneously claiming pending events (no duplicate claims).
func TestJobsManager_Outbox_SimultaneousClaims(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	const count = 30
	for i := 0; i < count; i++ {
		_, err := jm.PublishOutbox("event.claim", "topic", map[string]any{"i": i})
		if err != nil {
			t.Fatalf("PublishOutbox failed: %v", err)
		}
	}

	var claimCount atomic.Int64
	var claimsMap sync.Map
	var duplicates atomic.Int64
	var wg sync.WaitGroup

	for w := 0; w < 6; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = jm.ProcessPendingOutboxWithLease(count, 30*time.Second, func(evt *core.OutboxEvent) error {
				claimCount.Add(1)
				if _, loaded := claimsMap.LoadOrStore(evt.Id, true); loaded {
					duplicates.Add(1)
				}
				time.Sleep(5 * time.Millisecond)
				return nil
			})
		}()
	}
	wg.Wait()

	if duplicates.Load() != 0 {
		t.Fatalf("expected 0 duplicate claims across concurrent workers, got %d", duplicates.Load())
	}
	if claimCount.Load() != count {
		t.Fatalf("expected %d total claims, got %d", count, claimCount.Load())
	}
}

// 2. Old events created hours before their first processing attempt (not prematurely reclaimed).
func TestJobsManager_Outbox_OldEventsNotPrematurelyReclaimed(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("event.old", "topic", map[string]any{"key": "old"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	// Backdate the event's created timestamp to 5 hours ago
	_, err = app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[created]] = now() - INTERVAL '5 hours'
		WHERE [[id]] = {:id}
	`).Bind(map[string]any{"id": evt.Id}).Execute()
	if err != nil {
		t.Fatalf("failed to backdate event: %v", err)
	}

	// Worker 1 claims with a 60-second lease
	worker1Started := make(chan struct{})
	worker1Finish := make(chan struct{})
	worker2Claimed := atomic.Bool{}

	go func() {
		_, _ = jm.ProcessPendingOutboxWithLease(1, 60*time.Second, func(e *core.OutboxEvent) error {
			close(worker1Started)
			<-worker1Finish
			return nil
		})
	}()

	<-worker1Started

	// Worker 2 attempts to claim while Worker 1's lease is still active (even though created was 5 hours ago)
	n, err := jm.ProcessPendingOutboxWithLease(1, 60*time.Second, func(e *core.OutboxEvent) error {
		worker2Claimed.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("worker 2 claim error: %v", err)
	}
	if n != 0 || worker2Claimed.Load() {
		t.Fatalf("worker 2 prematurely reclaimed old event while worker 1 holds a valid lease")
	}

	close(worker1Finish)
}

// 3. A healthy publisher processing longer than the previous 60-second threshold.
func TestJobsManager_Outbox_LongRunningPublisherWithinLease(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("event.long", "topic", map[string]any{"key": "long"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	// Claim with a 120-second lease
	worker1Claimed := make(chan struct{})
	worker1Release := make(chan struct{})
	go func() {
		_, _ = jm.ProcessPendingOutboxWithLease(1, 120*time.Second, func(e *core.OutboxEvent) error {
			close(worker1Claimed)
			<-worker1Release
			return nil
		})
	}()

	<-worker1Claimed

	// Backdate created and leased_at to 70 seconds ago, BUT keep leased_until 50 seconds in the future
	_, err = app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[created]] = now() - INTERVAL '70 seconds',
		    [[leased_at]] = now() - INTERVAL '70 seconds',
		    [[leased_until]] = now() + INTERVAL '50 seconds'
		WHERE [[id]] = {:id}
	`).Bind(map[string]any{"id": evt.Id}).Execute()
	if err != nil {
		t.Fatalf("failed to backdate event: %v", err)
	}

	// Another worker attempts to claim: since leased_until > now(), it must NOT be reclaimed!
	n, err := jm.ProcessPendingOutbox(1, func(e *core.OutboxEvent) error {
		t.Errorf("event reclaimed while leased_until is in the future!")
		return nil
	})
	if err != nil {
		t.Fatalf("process error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 claimed events, got %d", n)
	}

	close(worker1Release)
}

// 4. A worker crashing after claiming an event (stale lease recovery).
func TestJobsManager_Outbox_WorkerCrashAndReclamation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("event.crash", "topic", map[string]any{"key": "crash"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	// Simulate crashed worker: status='processing', leased_until in the past
	_, err = app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[status]] = 'processing',
		    [[claim_token]] = 'crashed-worker-token',
		    [[leased_at]] = now() - INTERVAL '120 seconds',
		    [[leased_until]] = now() - INTERVAL '60 seconds'
		WHERE [[id]] = {:id}
	`).Bind(map[string]any{"id": evt.Id}).Execute()
	if err != nil {
		t.Fatalf("failed to simulate crashed worker: %v", err)
	}

	// Active worker reclaims the event
	reclaimed := false
	n, err := jm.ProcessPendingOutbox(1, func(e *core.OutboxEvent) error {
		if e.Id == evt.Id {
			reclaimed = true
			if e.ClaimToken == "crashed-worker-token" {
				t.Fatalf("expected new claim token, got old token %s", e.ClaimToken)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reclamation error: %v", err)
	}
	if n != 1 || !reclaimed {
		t.Fatalf("failed to reclaim crashed worker event, n=%d", n)
	}
}

// 5. Lease expiration followed by another worker reclaiming the event (claim token rotation).
func TestJobsManager_Outbox_LeaseExpirationAndTokenRotation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("event.rotate", "topic", map[string]any{"key": "rotate"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	var token1 string
	// Claim 1: short lease
	_, _ = jm.ProcessPendingOutboxWithLease(1, 1*time.Second, func(e *core.OutboxEvent) error {
		if e.Id != evt.Id {
			t.Errorf("expected evt id %s, got %s", evt.Id, e.Id)
		}
		token1 = e.ClaimToken
		return errors.New("temporary failure") // goes back to pending
	})

	if token1 == "" {
		t.Fatalf("expected non-empty claim token on first claim")
	}

	var token2 string
	// Claim 2: second claim
	_, _ = jm.ProcessPendingOutboxWithLease(1, 1*time.Second, func(e *core.OutboxEvent) error {
		token2 = e.ClaimToken
		return nil
	})

	if token2 == "" {
		t.Fatalf("expected non-empty claim token on second claim")
	}
	if token1 == token2 {
		t.Fatalf("expected new claim token upon reclamation, got identical tokens %s", token1)
	}
}

// 6. A stale worker attempting to mark a reclaimed event as published.
func TestJobsManager_Outbox_StaleWorkerCannotMarkPublished(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("event.stale.pub", "topic", map[string]any{"key": "stale"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	// 1. Worker 1 claims event
	var evt1 *core.OutboxEvent
	_, _ = jm.ProcessPendingOutboxWithLease(1, 60*time.Second, func(e *core.OutboxEvent) error {
		if e.Id != evt.Id {
			t.Errorf("expected evt id %s, got %s", evt.Id, e.Id)
		}
		evt1 = e
		return errors.New("simulated pause/delay") // will reset to pending for test setup
	})

	// 2. Worker 2 claims event and successfully completes it
	n2, err := jm.ProcessPendingOutboxWithLease(1, 60*time.Second, func(e *core.OutboxEvent) error {
		return nil // successfully publishes
	})
	if err != nil || n2 != 1 {
		t.Fatalf("worker 2 failed to publish: n=%d err=%v", n2, err)
	}

	// 3. Stale Worker 1 attempts to publish using its old claim token
	res, updateErr := app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[status]] = 'published',
		    [[published_at]] = now()
		WHERE [[id]] = {:id}
		  AND [[claim_token]] = {:claimToken}
		  AND [[status]] = 'processing'
	`).Bind(map[string]any{
		"id":         evt1.Id,
		"claimToken": evt1.ClaimToken,
	}).Execute()

	if updateErr != nil {
		t.Fatalf("query error: %v", updateErr)
	}
	affected, _ := res.RowsAffected()
	if affected != 0 {
		t.Fatalf("expected 0 rows affected for stale worker publish attempt, got %d", affected)
	}
}

// 7. A stale worker attempting to overwrite a newer worker's failure status.
func TestJobsManager_Outbox_StaleWorkerCannotOverwriteFailureStatus(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("event.stale.fail", "topic", map[string]any{"key": "fail"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	// 1. Worker 1 claims event with stale token
	staleToken := "stale-worker-token"
	_, _ = app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[status]] = 'processing',
		    [[claim_token]] = {:token},
		    [[leased_at]] = now() - INTERVAL '100s',
		    [[leased_until]] = now() - INTERVAL '10s'
		WHERE [[id]] = {:id}
	`).Bind(map[string]any{"id": evt.Id, "token": staleToken}).Execute()

	// 2. Newer Worker 2 claims and fails (increments attempts to 2, records new last_error)
	n2, err := jm.ProcessPendingOutbox(1, func(e *core.OutboxEvent) error {
		return errors.New("worker 2 failure")
	})
	if err != nil || n2 != 0 {
		t.Fatalf("worker 2 unexpected result: n=%d, err=%v", n2, err)
	}

	// 3. Stale worker attempts to execute failure transition using staleToken
	res, updateErr := app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[status]] = 'failed',
		    [[last_error]] = 'stale error overwrite'
		WHERE [[id]] = {:id}
		  AND [[claim_token]] = {:token}
		  AND [[status]] = 'processing'
	`).Bind(map[string]any{"id": evt.Id, "token": staleToken}).Execute()

	if updateErr != nil {
		t.Fatalf("query error: %v", updateErr)
	}
	affected, _ := res.RowsAffected()
	if affected != 0 {
		t.Fatalf("expected 0 rows affected by stale worker failure update, got %d", affected)
	}

	// Verify DB holds worker 2 error, not stale worker error
	var lastErr string
	_ = app.DB().NewQuery("SELECT [[last_error]] FROM {{_outbox_events}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": evt.Id}).Row(&lastErr)
	if lastErr == "stale error overwrite" {
		t.Fatalf("stale worker overwrote failure status!")
	}
}

// 8. A publisher succeeding externally before an acknowledgment failure (at-least-once recovery).
func TestJobsManager_Outbox_ExternalSuccessBeforeAckFailure(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("event.ack.fail", "topic", map[string]any{"idempotencyKey": "order_123"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	externalDispatchCount := 0
	// Worker dispatch succeeds externally, but crash simulation leaves event in processing with expired lease
	externalDispatchCount++
	_, err = app.DB().NewQuery(`
		UPDATE {{_outbox_events}}
		SET [[status]] = 'processing',
		    [[leased_until]] = now() - INTERVAL '5 seconds'
		WHERE [[id]] = {:id}
	`).Bind(map[string]any{"id": evt.Id}).Execute()
	if err != nil {
		t.Fatalf("failed to simulate unacknowledged external dispatch: %v", err)
	}

	// Subsequent worker reclaims and completes external dispatch idempotently
	n, err := jm.ProcessPendingOutbox(1, func(e *core.OutboxEvent) error {
		externalDispatchCount++
		return nil
	})
	if err != nil || n != 1 {
		t.Fatalf("failed at-least-once recovery: n=%d err=%v", n, err)
	}

	// Verify external dispatched twice (at-least-once semantics) and DB reached published
	if externalDispatchCount != 2 {
		t.Fatalf("expected 2 external dispatches demonstrating at-least-once delivery, got %d", externalDispatchCount)
	}

	var status string
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_outbox_events}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": evt.Id}).Row(&status)
	if status != "published" {
		t.Fatalf("expected status 'published', got %s", status)
	}
}

// 9. PostgreSQL errors during claim, completion, and retry transitions.
func TestJobsManager_Outbox_DBErrorHandling(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	_, err = jm.PublishOutbox("event.dberr", "topic", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	// Limit <= 0 should gracefully default to 50 without error
	n, err := jm.ProcessPendingOutbox(-5, func(e *core.OutboxEvent) error {
		return nil
	})
	if err != nil {
		t.Fatalf("expected graceful handling of negative limit, got err: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 published event, got %d", n)
	}
}

// 10. Concurrent processing across separate Ruxbase application instances.
func TestJobsManager_Outbox_MultiInstanceConcurrentProcessing(t *testing.T) {
	app1, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app 1: %v", err)
	}
	defer app1.Cleanup()

	// Create second app instance sharing the same database URL
	app2 := core.NewBaseApp(core.BaseAppConfig{
		DataDir:          app1.DataDir(),
		DatabaseURL:      app1.DatabaseURL(),
		DataMaxOpenConns: 4,
		AuxMaxOpenConns:  2,
	})
	if err := app2.Bootstrap(); err != nil {
		t.Fatalf("failed to bootstrap app 2: %v", err)
	}
	defer app2.ClearBootstrap()

	jm1 := app1.Jobs()
	jm2 := app2.Jobs()

	const totalEvents = 40
	for i := 0; i < totalEvents; i++ {
		_, err := jm1.PublishOutbox("event.multi", "topic", map[string]any{"seq": i})
		if err != nil {
			t.Fatalf("PublishOutbox failed: %v", err)
		}
	}

	var processed1, processed2 atomic.Int64
	var seen sync.Map
	var duplicates atomic.Int64
	var wg sync.WaitGroup

	// Run workers across both instances simultaneously
	wg.Add(2)
	go func() {
		defer wg.Done()
		for {
			n, err := jm1.ProcessPendingOutbox(10, func(e *core.OutboxEvent) error {
				if _, exists := seen.LoadOrStore(e.Id, true); exists {
					duplicates.Add(1)
				}
				processed1.Add(1)
				time.Sleep(3 * time.Millisecond)
				return nil
			})
			if err != nil || n == 0 {
				return
			}
		}
	}()

	go func() {
		defer wg.Done()
		for {
			n, err := jm2.ProcessPendingOutbox(10, func(e *core.OutboxEvent) error {
				if _, exists := seen.LoadOrStore(e.Id, true); exists {
					duplicates.Add(1)
				}
				processed2.Add(1)
				time.Sleep(3 * time.Millisecond)
				return nil
			})
			if err != nil || n == 0 {
				return
			}
		}
	}()

	wg.Wait()

	if duplicates.Load() != 0 {
		t.Fatalf("cross-instance duplicate deliveries detected: %d", duplicates.Load())
	}
	totalProcessed := processed1.Load() + processed2.Load()
	if totalProcessed != totalEvents {
		t.Fatalf("expected %d total processed events across both instances, got %d (app1=%d, app2=%d)",
			totalEvents, totalProcessed, processed1.Load(), processed2.Load())
	}
}
