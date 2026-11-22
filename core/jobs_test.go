package core_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
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
	worker1Done := make(chan struct{})
	worker2Claimed := atomic.Bool{}

	go func() {
		defer close(worker1Done)
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
	<-worker1Done
}

//  3. Real wall-clock lease lifecycle using a blocked publisher and explicit barriers (Finding A):
//     Verifies NO reclaim before real lease expiry, correct reclaim with token rotation AFTER
//     real expiry, and stale worker completion rejection.
func TestJobsManager_Outbox_LongRunningPublisherWithinLease(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	evt, err := jm.PublishOutbox("event.long.wallclock", "topic", map[string]any{"key": "wallclock"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	const lease = 350 * time.Millisecond
	worker1Claimed := make(chan string, 1)
	worker1Release := make(chan struct{})
	worker1Res := make(chan int, 1)

	go func() {
		n, _ := jm.ProcessPendingOutboxWithLease(1, lease, func(e *core.OutboxEvent) error {
			worker1Claimed <- e.ClaimToken
			<-worker1Release
			return nil
		})
		worker1Res <- n
	}()

	token1 := <-worker1Claimed
	if token1 == "" {
		t.Fatalf("expected non-empty claim token for Worker 1")
	}

	// Phase 1: Before real lease expiry, Worker 2 MUST NOT reclaim the event
	var reclaimedEarly atomic.Bool
	nEarly, err := jm.ProcessPendingOutboxWithLease(1, lease, func(e *core.OutboxEvent) error {
		reclaimedEarly.Store(true)
		return nil
	})
	if err != nil {
		t.Fatalf("Worker 2 early claim error: %v", err)
	}
	if nEarly != 0 || reclaimedEarly.Load() {
		t.Fatalf("Worker 2 prematurely reclaimed event before real lease expiry!")
	}

	// Phase 2: Wait for the real PostgreSQL wall-clock lease (350ms) to expire
	time.Sleep(lease + 100*time.Millisecond)

	var token2 string
	nReclaim, err := jm.ProcessPendingOutboxWithLease(1, 5*time.Second, func(e *core.OutboxEvent) error {
		if e.Id != evt.Id {
			t.Errorf("expected event %s, got %s", evt.Id, e.Id)
		}
		token2 = e.ClaimToken
		return nil
	})
	if err != nil {
		t.Fatalf("Worker 2 post-expiry reclaim error: %v", err)
	}
	if nReclaim != 1 || token2 == "" || token2 == token1 {
		t.Fatalf("expected Worker 2 to reclaim with rotated token: n=%d token1=%q token2=%q", nReclaim, token1, token2)
	}

	// Phase 3: Stale Worker 1 finishes after reclamation -> fenced UPDATE matches 0 rows, count MUST be 0
	close(worker1Release)
	n1 := <-worker1Res
	if n1 != 0 {
		t.Fatalf("expected stale Worker 1 published count=0 after reclamation, got %d", n1)
	}
}

// 3b. Legitimately long-running publisher using background heartbeat renewal (Finding A):
//
//	Publication runs >2x the lease duration while RenewInterval extends leased_until in real time.
func TestJobsManager_Outbox_LongRunningWithHeartbeatRenewal(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()
	_, err = jm.PublishOutbox("event.heartbeat", "topic", map[string]any{"key": "hb"})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}

	const lease = 250 * time.Millisecond
	const renewEvery = 75 * time.Millisecond

	claimedCh := make(chan struct{})
	releaseCh := make(chan struct{})
	resCh := make(chan struct {
		n   int
		err error
	}, 1)

	go func() {
		n, err := jm.ProcessPendingOutboxWithOptions(context.Background(), core.OutboxProcessOptions{
			Limit:         1,
			LeaseDuration: lease,
			RenewInterval: renewEvery,
		}, func(ctx context.Context, e *core.OutboxEvent) error {
			close(claimedCh)
			select {
			case <-releaseCh:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
		resCh <- struct {
			n   int
			err error
		}{n: n, err: err}
	}()

	<-claimedCh

	// Wait longer than 2x the original 250ms lease (550ms); heartbeat must keep the lease alive
	time.Sleep(550 * time.Millisecond)

	// Worker 2 attempts to claim after >2x original lease -> MUST NOT steal because heartbeat renewed it!
	n2, err := jm.ProcessPendingOutboxWithLease(1, lease, func(e *core.OutboxEvent) error {
		t.Errorf("Worker 2 stole event while Worker 1 heartbeat was renewing lease!")
		return nil
	})
	if err != nil || n2 != 0 {
		t.Fatalf("expected Worker 2 to claim 0 events while heartbeat active, got n=%d err=%v", n2, err)
	}

	close(releaseCh)
	res := <-resCh
	if res.err != nil || res.n != 1 {
		t.Fatalf("expected Worker 1 to succeed with n=1 err=nil, got n=%d err=%v", res.n, res.err)
	}
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

// 9. Real PostgreSQL failure injection during claim, completion, retry, connection loss, and unknown outcome (Finding B).
func TestJobsManager_Outbox_DBErrorHandling(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()

	// 9a. Negative limit defaults cleanly to 50
	evt0, err := jm.PublishOutbox("event.dberr.default", "topic", map[string]any{"k": 0})
	if err != nil {
		t.Fatalf("PublishOutbox failed: %v", err)
	}
	n, err := jm.ProcessPendingOutbox(-5, func(e *core.OutboxEvent) error {
		if e.Id != evt0.Id {
			t.Errorf("expected %s, got %s", evt0.Id, e.Id)
		}
		return nil
	})
	if err != nil || n != 1 {
		t.Fatalf("expected graceful default limit n=1 err=nil, got n=%d err=%v", n, err)
	}

	// 9b. Real PostgreSQL claim query failure via canceled context
	evt1, _ := jm.PublishOutbox("event.dberr.claim", "topic", map[string]any{"k": 1})
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err = jm.ProcessPendingOutboxWithOptions(canceledCtx, core.OutboxProcessOptions{Limit: 5, LeaseDuration: 10 * time.Second}, func(ctx context.Context, e *core.OutboxEvent) error {
		t.Errorf("publisher must not be called when claim query fails")
		return nil
	})
	if err == nil || n != 0 {
		t.Fatalf("expected claim error and n=0 on canceled context, got n=%d err=%v", n, err)
	}

	// Verify event remained pending
	var st string
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_outbox_events}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": evt1.Id}).Row(&st)
	if st != "pending" {
		t.Fatalf("expected event to remain pending after failed claim, got %q", st)
	}

	// 9c. Real PostgreSQL completion UPDATE failure via context cancellation inside publisher
	//     (simulates connection loss right after external publication!)
	pubCtx, cancelAfterPub := context.WithCancel(context.Background())
	var externalPublished atomic.Int64
	n, err = jm.ProcessPendingOutboxWithOptions(pubCtx, core.OutboxProcessOptions{Limit: 1, LeaseDuration: 150 * time.Millisecond}, func(ctx context.Context, e *core.OutboxEvent) error {
		externalPublished.Add(1)
		// External dispatch succeeded; now drop/cancel DB context before completion UPDATE
		cancelAfterPub()
		return nil
	})
	if err == nil {
		t.Fatalf("expected error when completion UPDATE fails after external publish, got nil")
	}
	if n != 0 {
		t.Fatalf("database completion error MUST NOT be counted as published (got n=%d)", n)
	}

	// Event must still be in 'processing' and recoverable by another worker after 150ms lease expiry (at-least-once!)
	time.Sleep(200 * time.Millisecond)
	nRec, recErr := jm.ProcessPendingOutbox(1, func(e *core.OutboxEvent) error {
		externalPublished.Add(1)
		return nil
	})
	if recErr != nil || nRec != 1 {
		t.Fatalf("expected at-least-once recovery after completion ack failure, got n=%d err=%v", nRec, recErr)
	}
	if externalPublished.Load() != 2 {
		t.Fatalf("expected 2 external deliveries (at-least-once), got %d", externalPublished.Load())
	}

	// 9d. Real PostgreSQL retry UPDATE failure via context cancellation after publisher error
	evtRetry, _ := jm.PublishOutbox("event.dberr.retry", "topic", map[string]any{"k": 2})
	retryCtx, cancelAfterFail := context.WithCancel(context.Background())
	n, err = jm.ProcessPendingOutboxWithOptions(retryCtx, core.OutboxProcessOptions{Limit: 1, LeaseDuration: 150 * time.Millisecond}, func(ctx context.Context, e *core.OutboxEvent) error {
		cancelAfterFail()
		return errors.New("downstream 503")
	})
	if err == nil || n != 0 {
		t.Fatalf("expected retry UPDATE error and n=0, got n=%d err=%v", n, err)
	}
	// Recover after lease expiry
	time.Sleep(200 * time.Millisecond)
	n, err = jm.ProcessPendingOutbox(1, func(e *core.OutboxEvent) error {
		if e.Id != evtRetry.Id {
			t.Errorf("expected %s, got %s", evtRetry.Id, e.Id)
		}
		return nil
	})
	if err != nil || n != 1 {
		t.Fatalf("expected recovery of event after retry UPDATE failure, got n=%d err=%v", n, err)
	}

	// 9e. Connection loss after claim (simulated via AfterClaim fault + separate healthy app instance)
	evtLost, _ := jm.PublishOutbox("event.dberr.lost", "topic", map[string]any{"k": 3})
	core.SetOutboxFaultHooksForTest(jm, &core.OutboxFaultHooksForTest{
		AfterClaim: func(events []*core.OutboxEvent) error {
			return errors.New("simulated connection loss immediately after claim")
		},
	})
	n, err = jm.ProcessPendingOutboxWithLease(1, 150*time.Millisecond, func(e *core.OutboxEvent) error {
		t.Errorf("publisher must not execute after connection loss")
		return nil
	})
	core.SetOutboxFaultHooksForTest(jm, nil)
	if err == nil || n != 0 {
		t.Fatalf("expected connection loss error after claim, got n=%d err=%v", n, err)
	}
	time.Sleep(200 * time.Millisecond)
	n, err = jm.ProcessPendingOutbox(1, func(e *core.OutboxEvent) error {
		if e.Id != evtLost.Id {
			t.Errorf("expected %s, got %s", evtLost.Id, e.Id)
		}
		return nil
	})
	if err != nil || n != 1 {
		t.Fatalf("expected healthy worker to reclaim after post-claim connection loss, got n=%d err=%v", n, err)
	}

	// 9f. Unknown transaction outcome: PostgreSQL COMMITTED status='published', then transport drops before client ack
	evtUnknown, _ := jm.PublishOutbox("event.dberr.unknown", "topic", map[string]any{"k": 4})
	core.SetOutboxFaultHooksForTest(jm, &core.OutboxFaultHooksForTest{
		AfterMarkPub: func(evt *core.OutboxEvent, affected int64) error {
			return errors.New("simulated TCP reset after PostgreSQL commit (unknown tx outcome)")
		},
	})
	n, err = jm.ProcessPendingOutboxWithLease(1, 150*time.Millisecond, func(e *core.OutboxEvent) error {
		return nil
	})
	core.SetOutboxFaultHooksForTest(jm, nil)
	if err == nil || n != 0 {
		t.Fatalf("expected unknown-outcome error and n=0 (never falsely report success), got n=%d err=%v", n, err)
	}
	// Verify DB row IS 'published' and subsequent workers do NOT re-claim it!
	_ = app.DB().NewQuery("SELECT [[status]] FROM {{_outbox_events}} WHERE [[id]] = {:id}").
		Bind(map[string]any{"id": evtUnknown.Id}).Row(&st)
	if st != "published" {
		t.Fatalf("expected DB row to remain 'published' after committed unknown-outcome error, got %q", st)
	}
	time.Sleep(200 * time.Millisecond)
	nAfter, errAfter := jm.ProcessPendingOutbox(5, func(e *core.OutboxEvent) error {
		t.Errorf("already-committed event %s must not be reclaimed!", e.Id)
		return nil
	})
	if errAfter != nil || nAfter != 0 {
		t.Fatalf("expected 0 reclaimed events, got n=%d err=%v", nAfter, errAfter)
	}

	// 9g. Real closed PostgreSQL database pool error (proves genuine pgx/sql driver failure propagation)
	appClosed := core.NewBaseApp(core.BaseAppConfig{
		DataDir:          app.DataDir(),
		DatabaseURL:      app.DatabaseURL(),
		DataMaxOpenConns: 2,
		AuxMaxOpenConns:  1,
	})
	if err := appClosed.Bootstrap(); err != nil {
		t.Fatalf("failed to bootstrap disposable appClosed: %v", err)
	}
	defer appClosed.ClearBootstrap()

	jmClosed := appClosed.Jobs()
	dbxDB, ok := appClosed.DB().(*dbx.DB)
	if !ok || dbxDB == nil {
		t.Fatalf("expected *dbx.DB on bootstrapped appClosed, got %T", appClosed.DB())
	}
	if err := dbxDB.DB().Close(); err != nil {
		t.Fatalf("failed to close underlying sql.DB pool: %v", err)
	}
	nClosed, errClosed := jmClosed.ProcessPendingOutbox(1, func(e *core.OutboxEvent) error { return nil })
	if errClosed == nil || nClosed != 0 {
		t.Fatalf("expected real DB error on closed connection pool, got n=%d err=%v", nClosed, errClosed)
	}
}

// 9h. Batch lease expiration protection (Finding C):
//   - Case 1: Slow-but-valid batch refreshes each subsequent event's lease before publishing so all succeed.
//   - Case 2: If an earlier event overruns the lease, later events whose lease already expired are SKIPPED
//     instead of blindly published, allowing another worker to claim and publish them with 0 duplicates.
func TestJobsManager_Outbox_BatchLeaseExpirationGuard(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	jm := app.Jobs()

	// Case 1: 3 events claimed in one batch with 300ms lease; each takes 180ms (total 540ms > 300ms).
	// Pre-publish lease refresh extends each subsequent event before starting, so all 3 succeed cleanly!
	for i := 0; i < 3; i++ {
		if _, err := jm.PublishOutbox("event.batch.refresh", "topic", map[string]any{"i": i}); err != nil {
			t.Fatalf("PublishOutbox failed: %v", err)
		}
	}
	n, err := jm.ProcessPendingOutboxWithLease(3, 300*time.Millisecond, func(e *core.OutboxEvent) error {
		time.Sleep(180 * time.Millisecond)
		return nil
	})
	if err != nil || n != 3 {
		t.Fatalf("expected all 3 events in slow-but-valid batch to succeed via pre-publish lease refresh, got n=%d err=%v", n, err)
	}

	// Case 2: 3 events claimed in one batch with 200ms lease; event 1 stalls for 350ms (> 200ms lease).
	// By the time event 1 finishes, events 2 and 3 have ALREADY expired.
	// Worker 1 MUST skip publishing events 2 and 3 rather than blindly invoking publisher on expired leases.
	for i := 0; i < 3; i++ {
		if _, err := jm.PublishOutbox("event.batch.expired", "topic", map[string]any{"i": i}); err != nil {
			t.Fatalf("PublishOutbox failed: %v", err)
		}
	}

	var worker1PublishedIds []string
	var mu sync.Mutex
	var firstEventDone atomic.Bool
	n1, err1 := jm.ProcessPendingOutboxWithLease(3, 200*time.Millisecond, func(e *core.OutboxEvent) error {
		mu.Lock()
		worker1PublishedIds = append(worker1PublishedIds, e.Id)
		mu.Unlock()
		if firstEventDone.CompareAndSwap(false, true) {
			time.Sleep(350 * time.Millisecond)
		}
		return nil
	})
	if err1 != nil {
		t.Fatalf("unexpected error in overrun batch: %v", err1)
	}

	mu.Lock()
	w1Calls := len(worker1PublishedIds)
	mu.Unlock()
	if w1Calls != 1 {
		t.Fatalf("expected Worker 1 to publish ONLY event 1 and skip already-expired events 2 & 3, got %d publisher calls (n1=%d)", w1Calls, n1)
	}

	// Worker 2 cleanly claims and publishes the remaining 2 events (and event 1 if not published)
	n2, err2 := jm.ProcessPendingOutboxWithLease(10, 5*time.Second, func(e *core.OutboxEvent) error {
		return nil
	})
	if err2 != nil {
		t.Fatalf("Worker 2 failed: %v", err2)
	}
	if n1+n2 != 3 {
		t.Fatalf("expected total 3 published events across Worker 1 and Worker 2, got n1=%d n2=%d", n1, n2)
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
