package e2e_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/core/cluster"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/subscriptions"
)

type realtimePayload struct {
	Action string         `json:"action"`
	Record map[string]any `json:"record"`
}

func TestRealtime_RollbackEmitsNoEvents(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	if _, err := apis.NewRouter(app); err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	// Create collection for realtime tests
	col := core.NewBaseCollection("realtime_notes")
	emptyRule := ""
	col.ListRule = &emptyRule
	col.ViewRule = &emptyRule
	col.CreateRule = &emptyRule
	if err := app.Save(col); err != nil {
		t.Fatalf("Failed to create collection: %v", err)
	}

	// Connect test client
	client := subscriptions.NewDefaultClient()
	app.SubscriptionsBroker().Register(client)
	defer app.SubscriptionsBroker().Unregister(client.Id())

	client.Subscribe("realtime_notes")

	// Execute transaction that fails and rolls back
	_ = app.RunInTransaction(func(txApp core.App) error {
		rec := core.NewRecord(col)
		rec.Set("id", "rolledback00001")
		if err := txApp.Save(rec); err != nil {
			return err
		}
		// Return error to trigger rollback
		return errors.New("simulated transaction failure for rollback test")
	})

	// Verify no event arrived within 200ms
	select {
	case msg := <-client.Channel():
		t.Fatalf("Violation of committed-event boundary! Received uncommitted event: %s", string(msg.Data))
	case <-time.After(200 * time.Millisecond):
		// Success: zero events delivered for rolled back transaction
	}
}

func TestRealtime_CommittedTransactionEmitsInOrder(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	if _, err := apis.NewRouter(app); err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}

	col := core.NewBaseCollection("ordered_notes")
	emptyRule := ""
	col.ListRule = &emptyRule
	col.ViewRule = &emptyRule
	col.CreateRule = &emptyRule
	if err := app.Save(col); err != nil {
		t.Fatalf("Failed to save collection: %v", err)
	}

	client := subscriptions.NewDefaultClient()
	app.SubscriptionsBroker().Register(client)
	defer app.SubscriptionsBroker().Unregister(client.Id())

	client.Subscribe("ordered_notes")

	// Insert 3 records in order with valid 15-char IDs
	for i := 1; i <= 3; i++ {
		rec := core.NewRecord(col)
		rec.Set("id", fmt.Sprintf("ordertest00000%d", i))
		if err := app.Save(rec); err != nil {
			t.Fatalf("Failed to save record %d: %v", i, err)
		}
	}

	// Read 3 events and assert sequential order
	for i := 1; i <= 3; i++ {
		select {
		case msg := <-client.Channel():
			var p realtimePayload
			if err := json.Unmarshal(msg.Data, &p); err != nil {
				t.Fatalf("Failed to parse realtime payload: %v", err)
			}
			expectedId := fmt.Sprintf("ordertest00000%d", i)
			if p.Record["id"] != expectedId {
				t.Errorf("Ordering violation! Expected record id %s, got %v", expectedId, p.Record["id"])
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("Timeout waiting for event %d", i)
		}
	}
}

func TestRealtime_ClusterDeduplication(t *testing.T) {
	// Verifies node-level sliding-window deduplication logic
	dedup := cluster.NewEventsDeduplicator(10)

	batch1 := []string{"event_1", "event_2", "event_3", "event_1"}
	filtered1 := dedup.Filter(batch1)
	expected1 := []string{"event_1", "event_2", "event_3"}
	if !slices.Equal(filtered1, expected1) {
		t.Errorf("Expected filtered batch %v, got %v", expected1, filtered1)
	}

	// Second batch includes redundant events
	batch2 := []string{"event_2", "event_4", "event_3", "event_5"}
	filtered2 := dedup.Filter(batch2)
	expected2 := []string{"event_4", "event_5"}
	if !slices.Equal(filtered2, expected2) {
		t.Errorf("Expected filtered batch %v, got %v", expected2, filtered2)
	}
}
