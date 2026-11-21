package e2e_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/subscriptions"
)

func TestRealtime_BroadcastEphemeralCrossNodes(t *testing.T) {
	nodes := newClusterNodes(t, 2)
	nodeA := nodes[0]
	nodeB := nodes[1]

	routerA, err := apis.NewRouter(nodeA)
	if err != nil {
		t.Fatalf("Failed to create routerA: %v", err)
	}
	muxA, err := routerA.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build muxA: %v", err)
	}

	// Register subscriber client on Node B
	clientB := subscriptions.NewDefaultClient()
	nodeB.SubscriptionsBroker().Register(clientB)
	defer nodeB.SubscriptionsBroker().Unregister(clientB.Id())

	clientB.Subscribe("broadcast:room1")

	// Post broadcast to Node A
	broadcastBody := `{"channel": "room1", "event": "chat", "payload": {"text": "hello from node A"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/realtime/broadcast", bytes.NewBufferString(broadcastBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	muxA.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200 from broadcast, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify Node B receives broadcast event across cluster without any DB table writes
	select {
	case msg := <-clientB.Channel():
		if msg.Name != "broadcast:room1" {
			t.Fatalf("Expected topic broadcast:room1, got %s", msg.Name)
		}
		var parsed struct {
			Event   string         `json:"event"`
			Channel string         `json:"channel"`
			Payload map[string]any `json:"payload"`
		}
		if err := json.Unmarshal(msg.Data, &parsed); err != nil {
			t.Fatalf("Failed to decode broadcast msg: %v", err)
		}
		if parsed.Event != "chat" || parsed.Payload["text"] != "hello from node A" {
			t.Fatalf("Unexpected broadcast payload content: %+v", parsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for broadcast message across cluster nodes")
	}
}

func TestRealtime_PresenceTrackingAndSync(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	// Register client 1
	client1 := subscriptions.NewDefaultClient()
	app.SubscriptionsBroker().Register(client1)
	defer app.SubscriptionsBroker().Unregister(client1.Id())
	client1.Subscribe("presence:lobby")

	// Client 1 tracks presence
	trackBody1 := `{"channel": "lobby", "clientId": "` + client1.Id() + `", "action": "track", "state": {"cursor": [10, 20]}}`
	req1 := httptest.NewRequest(http.MethodPost, "/api/realtime/presence", bytes.NewBufferString(trackBody1))
	req1.Header.Set("Content-Type", "application/json")
	rec1 := httptest.NewRecorder()
	mux.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200 for presence track 1, got %d: %s", rec1.Code, rec1.Body.String())
	}

	// Client 1 should receive sync event with 1 member
	select {
	case msg := <-client1.Channel():
		if msg.Name != "presence:lobby" {
			t.Fatalf("Expected presence:lobby, got %s", msg.Name)
		}
		var syncData struct {
			Event    string `json:"event"`
			Channel  string `json:"channel"`
			Presence []struct {
				SessionId string `json:"sessionId"`
			} `json:"presence"`
		}
		if err := json.Unmarshal(msg.Data, &syncData); err != nil {
			t.Fatalf("Failed to unmarshal presence msg: %v", err)
		}
		if len(syncData.Presence) != 1 || syncData.Presence[0].SessionId != client1.Id() {
			t.Fatalf("Expected 1 presence session with client1 ID, got: %+v", syncData.Presence)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for presence sync 1")
	}

	// Register client 2
	client2 := subscriptions.NewDefaultClient()
	app.SubscriptionsBroker().Register(client2)
	client2.Subscribe("presence:lobby")

	// Client 2 tracks presence
	trackBody2 := `{"channel": "lobby", "clientId": "` + client2.Id() + `", "action": "track", "state": {"cursor": [50, 60]}}`
	req2 := httptest.NewRequest(http.MethodPost, "/api/realtime/presence", bytes.NewBufferString(trackBody2))
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200 for presence track 2, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// Client 1 should receive sync event with 2 members
	select {
	case msg := <-client1.Channel():
		var syncData struct {
			Presence []struct {
				SessionId string `json:"sessionId"`
			} `json:"presence"`
		}
		_ = json.Unmarshal(msg.Data, &syncData)
		if len(syncData.Presence) != 2 {
			t.Fatalf("Expected 2 presence sessions, got: %d", len(syncData.Presence))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for presence sync 2")
	}

	// Untrack client 2 via untrack action
	untrackBody := `{"channel": "lobby", "clientId": "` + client2.Id() + `", "action": "untrack"}`
	reqUntrack := httptest.NewRequest(http.MethodPost, "/api/realtime/presence", bytes.NewBufferString(untrackBody))
	reqUntrack.Header.Set("Content-Type", "application/json")
	recUntrack := httptest.NewRecorder()
	mux.ServeHTTP(recUntrack, reqUntrack)

	if recUntrack.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200 for presence untrack, got %d: %s", recUntrack.Code, recUntrack.Body.String())
	}

	// Client 1 receives sync event with 1 member
	select {
	case msg := <-client1.Channel():
		var syncData struct {
			Presence []struct {
				SessionId string `json:"sessionId"`
			} `json:"presence"`
		}
		_ = json.Unmarshal(msg.Data, &syncData)
		if len(syncData.Presence) != 1 || syncData.Presence[0].SessionId != client1.Id() {
			t.Fatalf("Expected 1 presence session after untrack, got: %+v", syncData.Presence)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for presence sync 3 (untrack)")
	}
}

func TestRealtime_BroadcastTenantIsolation(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("Failed to create router: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("Failed to build mux: %v", err)
	}

	clientA := subscriptions.NewDefaultClient()
	clientA.Set(apis.RealtimeClientTenantKey, "tenant_alpha")
	app.SubscriptionsBroker().Register(clientA)
	defer app.SubscriptionsBroker().Unregister(clientA.Id())
	clientA.Subscribe("broadcast:alerts")

	clientB := subscriptions.NewDefaultClient()
	clientB.Set(apis.RealtimeClientTenantKey, "tenant_beta")
	app.SubscriptionsBroker().Register(clientB)
	defer app.SubscriptionsBroker().Unregister(clientB.Id())
	clientB.Subscribe("broadcast:alerts")

	// Broadcast sent to tenant_alpha
	broadcastBody := `{"channel": "alerts", "event": "warning", "payload": {"msg": "alpha only"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/realtime/broadcast", bytes.NewBufferString(broadcastBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-Id", "tenant_alpha")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected HTTP 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Client A should receive
	select {
	case msg := <-clientA.Channel():
		if msg.Name != "broadcast:alerts" {
			t.Fatalf("Expected broadcast:alerts, got %s", msg.Name)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Client A did not receive tenant broadcast")
	}

	// Client B should NOT receive (isolated by tenant)
	select {
	case msg := <-clientB.Channel():
		t.Fatalf("Tenant isolation violation: client B received tenant A's broadcast: %s", msg.Data)
	case <-time.After(200 * time.Millisecond):
		// Success: client B did not receive tenant A's broadcast
	}
}
