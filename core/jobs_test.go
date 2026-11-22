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
