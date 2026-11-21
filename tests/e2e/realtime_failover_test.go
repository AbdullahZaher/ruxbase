package e2e_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/core/cluster"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/subscriptions"
	"github.com/pocketbase/dbx"
)

func TestRealtime_DuplicateEventFilteringAcrossFailover(t *testing.T) {
	t.Parallel()

	dedup := cluster.NewEventsDeduplicator(100)

	// Initial batch with duplicates within the batch
	batch1 := []string{"ev_1", "ev_2", "ev_1", "ev_3", "ev_2", ""}
	filtered1 := dedup.Filter(batch1)

	expected1 := []string{"ev_1", "ev_2", "ev_3"}
	if len(filtered1) != len(expected1) {
		t.Fatalf("Expected 3 filtered events, got %d: %v", len(filtered1), filtered1)
	}
	for i, id := range expected1 {
		if filtered1[i] != id {
			t.Fatalf("Expected event %s at index %d, got %s", id, i, filtered1[i])
		}
	}

	// Replayed batch containing already seen events and new event
	batch2 := []string{"ev_1", "ev_2", "ev_4", "ev_3"}
	filtered2 := dedup.Filter(batch2)

	if len(filtered2) != 1 || filtered2[0] != "ev_4" {
		t.Fatalf("Expected only new event ev_4, got: %v", filtered2)
	}

	// Forget an event to simulate broadcast retry
	dedup.Forget([]string{"ev_2"})
	batch3 := []string{"ev_2", "ev_3"}
	filtered3 := dedup.Filter(batch3)

	if len(filtered3) != 1 || filtered3[0] != "ev_2" {
		t.Fatalf("Expected forgotten event ev_2 to be accepted again, got: %v", filtered3)
	}
}

func TestRealtime_ClusterListenReconnectUnderDisconnect(t *testing.T) {
	testDB, err := tests.NewFixturesTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(testDB.Name)

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{
		DatabaseURL: testDB.URL,
		ClusterMode: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	select {
	case <-app.Cluster().Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for cluster ready")
	}

	received := make(chan string, 1)
	app.Cluster().Subscribe("failover_topic", func(msg cluster.Message) {
		received <- msg.Payload
	})

	// 1. Publish baseline message
	if err := app.Cluster().Notify("failover_topic", "msg_before_drop"); err != nil {
		t.Fatal(err)
	}

	select {
	case p := <-received:
		if p != "msg_before_drop" {
			t.Fatalf("Expected msg_before_drop, got: %s", p)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for baseline message")
	}

	// 2. Kill the PostgreSQL backend PID running the LISTEN connection
	directDB, err := dbx.Open("pgx", testDB.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer directDB.Close()

	var terminated bool
	err = directDB.NewQuery(`
		SELECT pg_terminate_backend(pid)
		FROM pg_stat_activity
		WHERE datname = current_database()
		  AND query LIKE '%LISTEN%'
		  AND pid <> pg_backend_pid()
		LIMIT 1
	`).Row(&terminated)
	if err != nil {
		t.Logf("Notice: pg_terminate_backend result: %v", err)
	}

	// 3. Allow time for reconnect loop (minReconnectDelay is 100ms)
	time.Sleep(500 * time.Millisecond)

	// 4. Verify message delivery resumes after reconnect
	if err := app.Cluster().Notify("failover_topic", "msg_after_reconnect"); err != nil {
		t.Fatal(err)
	}

	select {
	case p := <-received:
		if p != "msg_after_reconnect" {
			t.Fatalf("Expected msg_after_reconnect, got: %s", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for post-reconnect message")
	}
}

func TestRealtime_AdvisoryLockLeaderHandover(t *testing.T) {
	testDB, err := tests.NewFixturesTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(testDB.Name)

	appA, err := tests.NewTestAppWithConfig(core.BaseAppConfig{
		DatabaseURL: testDB.URL,
		ClusterMode: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer appA.Cleanup()

	appB, err := tests.NewTestAppWithConfig(core.BaseAppConfig{
		DatabaseURL: testDB.URL,
		ClusterMode: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer appB.Cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	lockName := "test_failover_lock_" + fmt.Sprint(time.Now().UnixNano())

	// 1. Node A acquires advisory lock
	unlockA, err := appA.Cluster().TryLock(ctx, lockName)
	if err != nil {
		t.Fatalf("Failed to acquire lock on Node A: %v", err)
	}
	if unlockA == nil {
		t.Fatal("Expected Node A to acquire the lock")
	}

	// 2. Node B attempts to acquire the lock -> must fail (held by Node A)
	unlockB, err := appB.Cluster().TryLock(ctx, lockName)
	if err != nil {
		t.Fatalf("Error attempting lock on Node B: %v", err)
	}
	if unlockB != nil {
		unlockB()
		t.Fatal("Node B acquired lock while Node A holds it! Split brain detected.")
	}

	// 3. Verify lock status
	isLocked, err := appA.Cluster().IsLocked(ctx, lockName)
	if err != nil || !isLocked {
		t.Fatalf("Expected lock to be active, got %v, err %v", isLocked, err)
	}

	// 4. Node A releases lock (simulating graceful failover / crash release)
	unlockA()

	// 5. Node B attempts again -> must succeed
	unlockB, err = appB.Cluster().TryLock(ctx, lockName)
	if err != nil {
		t.Fatalf("Error acquiring lock on Node B after Node A release: %v", err)
	}
	if unlockB == nil {
		t.Fatal("Expected Node B to acquire lock after Node A released it")
	}
	defer unlockB()
}

func TestRealtime_EventFilteringByTopicAndNode(t *testing.T) {
	testDB, err := tests.NewFixturesTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(testDB.Name)

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{
		DatabaseURL: testDB.URL,
		ClusterMode: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Cleanup()

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}

	// 1. Create collection
	rule := ""
	col := core.NewBaseCollection("topic_filtered_posts")
	col.ListRule = &rule
	col.ViewRule = &rule
	col.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}

	// 2. Client subscribes ONLY to specific record topic "topic_filtered_posts/rec001000000000"
	client := subscriptions.NewDefaultClient()
	client.Subscribe("topic_filtered_posts/rec001000000000")
	app.SubscriptionsBroker().Register(client)

	// 3. Create a record with ID "rec002000000000" (not rec001000000000)
	rec2 := core.NewRecord(col)
	rec2.Id = "rec002000000000"
	rec2.Set("title", "Other record")
	if err := app.Save(rec2); err != nil {
		t.Fatal(err)
	}

	// 4. Verify client receives NOTHING (filtered out by topic)
	select {
	case msg := <-client.Channel():
		t.Fatalf("Expected topic filtering to discard rec002, but received: %s: %s", msg.Name, msg.Data)
	case <-time.After(500 * time.Millisecond):
		// Success: topic filtering correctly prevented delivery
	}

	// 5. Create matching record "rec001000000000"
	rec1 := core.NewRecord(col)
	rec1.Id = "rec001000000000"
	rec1.Set("title", "Matching record")
	if err := app.Save(rec1); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-client.Channel():
		if !strings.Contains(string(msg.Data), "rec001000000000") {
			t.Fatalf("Expected matching record rec001000000000, got: %s", msg.Data)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for matching topic message")
	}

	_ = mux
}
