package e2e_test

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/subscriptions"
)

func TestRealtime_ReconnectWithValidCursorReplaysEvents(t *testing.T) {
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

	<-app.Cluster().Ready()

	// 1. Create a public collection
	rule := ""
	col := core.NewBaseCollection("reconnect_posts")
	col.ListRule = &rule
	col.ViewRule = &rule
	col.Fields.Add(&core.TextField{Name: "title"})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}

	// 2. Insert baseline event and capture its timestamp cursor
	cursorTime := time.Now().Add(-5 * time.Second)
	cursorId := fmt.Sprintf("%d_baseline000", cursorTime.UnixMilli())

	// 3. Create 2 records while client is offline
	rec1 := core.NewRecord(col)
	rec1.Set("title", "Post Replay 1")
	if err := app.Save(rec1); err != nil {
		t.Fatal(err)
	}

	rec2 := core.NewRecord(col)
	rec2.Set("title", "Post Replay 2")
	if err := app.Save(rec2); err != nil {
		t.Fatal(err)
	}

	// 4. Reconnect with Last-Event-ID cursor
	req := httptest.NewRequest(http.MethodGet, "/api/realtime", nil)
	req.Header.Set("Last-Event-ID", cursorId)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(rec, req)
	}()

	// Wait for events to be written
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()

	// 5. Verify PB_CONNECT and replayed events
	if !strings.Contains(body, "event:PB_CONNECT") {
		t.Fatalf("Expected PB_CONNECT in response, got: %s", body)
	}
	if !strings.Contains(body, rec1.Id) {
		t.Fatalf("Expected replayed rec1 %s in response, got: %s", rec1.Id, body)
	}
	if !strings.Contains(body, rec2.Id) {
		t.Fatalf("Expected replayed rec2 %s in response, got: %s", rec2.Id, body)
	}
}

func TestRealtime_ReconnectWithExpiredCursorEmitsResyncRequired(t *testing.T) {
	testDB, err := tests.NewFixturesTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(testDB.Name)

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{
		DatabaseURL: testDB.URL,
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

	// Cursor older than 5 minutes (e.g. 10 minutes ago)
	expiredTime := time.Now().Add(-10 * time.Minute)
	expiredCursor := fmt.Sprintf("%d_oldcursor000", expiredTime.UnixMilli())

	req := httptest.NewRequest(http.MethodGet, "/api/realtime?lastEventId="+expiredCursor, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(rec, req)
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()
	<-done

	body := rec.Body.String()

	if !strings.Contains(body, "event:PB_CONNECT") {
		t.Fatalf("Expected PB_CONNECT in response, got: %s", body)
	}
	if !strings.Contains(body, "event:PB_RESYNC_REQUIRED") {
		t.Fatalf("Expected PB_RESYNC_REQUIRED in response for expired cursor, got: %s", body)
	}
	if !strings.Contains(body, "cursor_expired") {
		t.Fatalf("Expected cursor_expired in data payload, got: %s", body)
	}
}

func TestRealtime_BackpressureDisconnectsSlowConsumer(t *testing.T) {
	t.Parallel()

	// 1. Create a client with bounded buffer (256 messages)
	client := subscriptions.NewDefaultClient()

	if client.IsDiscarded() {
		t.Fatal("Newly created client should not be discarded")
	}

	// 2. Fill the buffer to capacity (256 messages) without reading
	for i := 0; i < subscriptions.DefaultChannelBufferSize; i++ {
		client.Send(subscriptions.Message{
			Name: "test/fill",
			Data: []byte(`{"i":` + fmt.Sprint(i) + `}`),
		})
	}

	if client.IsDiscarded() {
		t.Fatal("Client should not be discarded while filling up to capacity")
	}

	// 3. Send one more message exceeding the capacity (backpressure threshold)
	client.Send(subscriptions.Message{
		Name: "test/overflow",
		Data: []byte(`{"overflow":true}`),
	})

	// 4. Verify client was discarded by backpressure protection
	if !client.IsDiscarded() {
		t.Fatal("Expected slow client to be discarded when message channel overflows buffer capacity")
	}

	// Further sends to discarded client must be safe no-ops
	client.Send(subscriptions.Message{Name: "test/after_discard"})
}

func TestRealtime_MonotonicEventIdAssigned(t *testing.T) {
	testDB, err := tests.NewFixturesTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(testDB.Name)

	app, err := tests.NewTestAppWithConfig(core.BaseAppConfig{
		DatabaseURL: testDB.URL,
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

	// Create test collection
	rule := ""
	col := core.NewBaseCollection("monotonic_posts")
	col.ListRule = &rule
	col.ViewRule = &rule
	col.Fields.Add(&core.TextField{Name: "name"})
	if err := app.Save(col); err != nil {
		t.Fatal(err)
	}

	// Subscribe client
	client := subscriptions.NewDefaultClient()
	client.Subscribe("monotonic_posts/*")
	app.SubscriptionsBroker().Register(client)

	rec := core.NewRecord(col)
	rec.Set("name", "Test Item")
	if err := app.Save(rec); err != nil {
		t.Fatal(err)
	}

	select {
	case msg := <-client.Channel():
		if msg.Id == "" {
			t.Fatal("Expected non-empty Id on real-time event Message")
		}
		parts := strings.Split(msg.Id, "_")
		if len(parts) < 2 {
			t.Fatalf("Expected timestamp_entropy format for event Id, got: %s", msg.Id)
		}

		// Verify WriteSSE formats the id line correctly
		var buf strings.Builder
		if err := msg.WriteSSE(&buf, client.Id()); err != nil {
			t.Fatal(err)
		}
		out := buf.String()
		scanner := bufio.NewScanner(strings.NewReader(out))
		var idLine string
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), "id:") {
				idLine = scanner.Text()
				break
			}
		}
		if idLine != "id:"+msg.Id {
			t.Fatalf("Expected WriteSSE to output id:%s, got: %s", msg.Id, idLine)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for message on client channel")
	}

	_ = mux
}
