package e2e_test

import (
	"encoding/json/v2"
	"net/url"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/subscriptions"
)

func receiveClientMsg(t *testing.T, client subscriptions.Client, timeout time.Duration) (subscriptions.Message, bool) {
	select {
	case msg := <-client.Channel():
		return msg, true
	case <-time.After(timeout):
		return subscriptions.Message{}, false
	}
}

func expectClientMessage(t *testing.T, client subscriptions.Client, action string, recordId string, timeout time.Duration) {
	t.Helper()
	msg, ok := receiveClientMsg(t, client, timeout)
	if !ok {
		t.Fatalf("Expected %s event for record %s, got none within %v", action, recordId, timeout)
	}

	var data realtimePayload
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("Failed to parse event data: %v (raw: %s)", err, msg.Data)
	}

	if data.Action != action {
		t.Fatalf("Expected action %s, got %s", action, data.Action)
	}
	if data.Record["id"] != recordId {
		t.Fatalf("Expected record ID %s, got %v", recordId, data.Record["id"])
	}
}

func expectNoClientMessage(t *testing.T, client subscriptions.Client, waitDuration time.Duration) {
	t.Helper()
	if msg, ok := receiveClientMsg(t, client, waitDuration); ok {
		t.Fatalf("Expected no event, but received %s: %s", msg.Name, msg.Data)
	}
}

func newClusterNodes(t *testing.T, count int) []*tests.TestApp {
	testDB, err := tests.NewFixturesTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tests.DropTestDatabase(testDB.Name) })

	nodes := make([]*tests.TestApp, count)
	for i := range nodes {
		node, err := tests.NewTestAppWithConfig(core.BaseAppConfig{
			DatabaseURL: testDB.URL,
			ClusterMode: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(node.Cleanup)

		if _, err := apis.NewRouter(node); err != nil {
			t.Fatal(err)
		}

		select {
		case <-node.Cluster().Ready():
		case <-time.After(5 * time.Second):
			t.Fatal("Cluster node LISTEN timeout")
		}

		nodes[i] = node
	}

	return nodes
}

func TestRealtimeAuth_InstantTokenRevocationAcrossNodes(t *testing.T) {
	t.Parallel()

	nodes := newClusterNodes(t, 2)
	nodeA, nodeB := nodes[0], nodes[1]

	// 1. Create a private collection requiring authenticated user
	privateRule := "@request.auth.id != ''"
	col := core.NewBaseCollection("auth_notes")
	col.ListRule = &privateRule
	col.ViewRule = &privateRule
	col.Fields.Add(&core.TextField{Name: "content"})
	if err := nodeA.Save(col); err != nil {
		t.Fatal(err)
	}

	// 2. Fetch user Alice from fixtures
	userAlice, err := nodeA.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// 3. Register client on Node A with Alice's auth
	clientA := subscriptions.NewDefaultClient()
	clientA.Subscribe("auth_notes/*")
	clientA.Set(apis.RealtimeClientAuthKey, userAlice)
	nodeA.SubscriptionsBroker().Register(clientA)

	// 4. On Node B, create a note -> clientA on Node A must receive it
	note1 := core.NewRecord(col)
	note1.Set("content", "first private note")
	if err := nodeB.Save(note1); err != nil {
		t.Fatal(err)
	}
	expectClientMessage(t, clientA, "create", note1.Id, 5*time.Second)

	// 5. On Node B, rotate Alice's tokenKey (e.g. password reset / session revocation)
	aliceOnB, err := nodeB.FindRecordById("users", userAlice.Id)
	if err != nil {
		t.Fatal(err)
	}
	aliceOnB.RefreshTokenKey()
	if err := nodeB.Save(aliceOnB); err != nil {
		t.Fatal(err)
	}

	// Allow cluster notification to propagate
	time.Sleep(200 * time.Millisecond)

	// 6. On Node B, create note2 -> clientA on Node A must NOT receive it
	note2 := core.NewRecord(col)
	note2.Set("content", "second private note after revocation")
	if err := nodeB.Save(note2); err != nil {
		t.Fatal(err)
	}

	expectNoClientMessage(t, clientA, 1*time.Second)

	// 7. Verify clientA's auth state was unset
	if clientA.Get(apis.RealtimeClientAuthKey) != nil {
		t.Fatal("Expected clientA auth to be unset after tokenKey revocation across nodes")
	}
}

func TestRealtimeAuth_UserDeletionAcrossNodes(t *testing.T) {
	t.Parallel()

	nodes := newClusterNodes(t, 2)
	nodeA, nodeB := nodes[0], nodes[1]

	privateRule := "@request.auth.id != ''"
	col := core.NewBaseCollection("auth_docs")
	col.ListRule = &privateRule
	col.ViewRule = &privateRule
	col.Fields.Add(&core.TextField{Name: "title"})
	if err := nodeA.Save(col); err != nil {
		t.Fatal(err)
	}

	userBob, err := nodeA.FindAuthRecordByEmail("users", "test2@example.com")
	if err != nil {
		t.Fatal(err)
	}

	clientBob := subscriptions.NewDefaultClient()
	clientBob.Subscribe("auth_docs/*")
	clientBob.Set(apis.RealtimeClientAuthKey, userBob)
	nodeA.SubscriptionsBroker().Register(clientBob)

	// Create doc1 on Node B -> Bob on Node A receives it
	doc1 := core.NewRecord(col)
	doc1.Set("title", "Doc 1")
	if err := nodeB.Save(doc1); err != nil {
		t.Fatal(err)
	}
	expectClientMessage(t, clientBob, "create", doc1.Id, 5*time.Second)

	// Delete Bob on Node B
	bobOnB, err := nodeB.FindRecordById("users", userBob.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := nodeB.Delete(bobOnB); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	// Create doc2 on Node B -> Bob on Node A must NOT receive it
	doc2 := core.NewRecord(col)
	doc2.Set("title", "Doc 2 after deletion")
	if err := nodeB.Save(doc2); err != nil {
		t.Fatal(err)
	}

	expectNoClientMessage(t, clientBob, 1*time.Second)

	if clientBob.Get(apis.RealtimeClientAuthKey) != nil {
		t.Fatal("Expected clientBob auth to be unset after user deletion across nodes")
	}
}

func TestRealtimeAuth_TenantIsolationAcrossNodes(t *testing.T) {
	t.Parallel()

	nodes := newClusterNodes(t, 2)
	nodeA, nodeB := nodes[0], nodes[1]

	// 1. Create two tenants on Node A
	tenantAlpha, err := nodeA.CreateTenant("Alpha Corp", "alpha-corp")
	if err != nil {
		t.Fatal(err)
	}
	tenantBeta, err := nodeA.CreateTenant("Beta LLC", "beta-llc")
	if err != nil {
		t.Fatal(err)
	}

	// 2. Create tenant-scoped collection
	rule := "@request.auth.id != ''"
	orderCol := core.NewBaseCollection("tenant_orders")
	orderCol.ListRule = &rule
	orderCol.ViewRule = &rule
	orderCol.Fields.Add(&core.TextField{Name: "item"})
	orderCol.Fields.Add(&core.TextField{Name: core.TenantIdField, Required: true})
	if err := nodeA.Save(orderCol); err != nil {
		t.Fatal(err)
	}

	// 3. User 1 belongs to Alpha, User 2 belongs to Beta
	user1, err := nodeA.FindAuthRecordByEmail("users", "test@example.com")
	if err != nil {
		t.Fatal(err)
	}
	user2, err := nodeA.FindAuthRecordByEmail("users", "test2@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := nodeA.AddTenantMember(tenantAlpha.Id, user1.Id, "member"); err != nil {
		t.Fatal(err)
	}
	if _, err := nodeA.AddTenantMember(tenantBeta.Id, user2.Id, "member"); err != nil {
		t.Fatal(err)
	}

	// 4. Register client1 (Alpha) and client2 (Beta) on Node A
	clientAlpha := subscriptions.NewDefaultClient()
	clientAlpha.Subscribe("tenant_orders/*")
	clientAlpha.Set(apis.RealtimeClientAuthKey, user1)
	clientAlpha.Set(apis.RealtimeClientTenantKey, tenantAlpha.Id)
	nodeA.SubscriptionsBroker().Register(clientAlpha)

	clientBeta := subscriptions.NewDefaultClient()
	clientBeta.Subscribe("tenant_orders/*")
	clientBeta.Set(apis.RealtimeClientAuthKey, user2)
	clientBeta.Set(apis.RealtimeClientTenantKey, tenantBeta.Id)
	nodeA.SubscriptionsBroker().Register(clientBeta)

	// 5. On Node B, create an order in Tenant Alpha
	orderAlpha := core.NewRecord(orderCol)
	orderAlpha.Set("item", "Alpha Widget")
	orderAlpha.Set(core.TenantIdField, tenantAlpha.Id)
	if err := nodeB.Save(orderAlpha); err != nil {
		t.Fatal(err)
	}

	// Client Alpha must receive it
	expectClientMessage(t, clientAlpha, "create", orderAlpha.Id, 5*time.Second)
	// Client Beta must NOT receive it
	expectNoClientMessage(t, clientBeta, 500*time.Millisecond)

	// 6. On Node B, create an order in Tenant Beta
	orderBeta := core.NewRecord(orderCol)
	orderBeta.Set("item", "Beta Gadget")
	orderBeta.Set(core.TenantIdField, tenantBeta.Id)
	if err := nodeB.Save(orderBeta); err != nil {
		t.Fatal(err)
	}

	// Client Beta must receive it
	expectClientMessage(t, clientBeta, "create", orderBeta.Id, 5*time.Second)
	// Client Alpha must NOT receive it
	expectNoClientMessage(t, clientAlpha, 500*time.Millisecond)
}

func TestRealtimeAuth_RuleChangeRevokesAccess(t *testing.T) {
	t.Parallel()

	nodes := newClusterNodes(t, 2)
	nodeA, nodeB := nodes[0], nodes[1]

	// 1. Create a public collection
	publicRule := ""
	col := core.NewBaseCollection("bulletins")
	col.ListRule = &publicRule
	col.ViewRule = &publicRule
	col.Fields.Add(&core.TextField{Name: "headline"})
	if err := nodeA.Save(col); err != nil {
		t.Fatal(err)
	}

	// 2. Connect an unauthenticated guest client on Node A
	guestClient := subscriptions.NewDefaultClient()
	guestClient.Subscribe("bulletins/*")
	nodeA.SubscriptionsBroker().Register(guestClient)

	// 3. On Node B, create bulletin 1 -> guest receives it
	b1 := core.NewRecord(col)
	b1.Set("headline", "Public news")
	if err := nodeB.Save(b1); err != nil {
		t.Fatal(err)
	}
	expectClientMessage(t, guestClient, "create", b1.Id, 5*time.Second)

	// 4. On Node B, admin hardens rule: requires auth
	colOnB, err := nodeB.FindCollectionByNameOrId("bulletins")
	if err != nil {
		t.Fatal(err)
	}
	privateRule := "@request.auth.id != ''"
	colOnB.ListRule = &privateRule
	colOnB.ViewRule = &privateRule
	if err := nodeB.Save(colOnB); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)

	// 5. On Node B, create bulletin 2 -> guest on Node A must NOT receive it
	b2 := core.NewRecord(colOnB)
	b2.Set("headline", "Restricted secret news")
	if err := nodeB.Save(b2); err != nil {
		t.Fatal(err)
	}
	expectNoClientMessage(t, guestClient, 1*time.Second)
}

func TestRealtimeAuth_RecordUpdateViolatingRuleDropsDelivery(t *testing.T) {
	t.Parallel()

	nodes := newClusterNodes(t, 2)
	nodeA, nodeB := nodes[0], nodes[1]

	rule := ""
	taskCol := core.NewBaseCollection("status_tasks")
	taskCol.ListRule = &rule
	taskCol.ViewRule = &rule
	taskCol.Fields.Add(&core.TextField{Name: "title"})
	taskCol.Fields.Add(&core.TextField{Name: "status"})
	if err := nodeA.Save(taskCol); err != nil {
		t.Fatal(err)
	}

	// Client subscribes with filter: status = 'pending'
	subOpt := url.QueryEscape(`{"query":{"filter":"status='pending'"}}`)
	client := subscriptions.NewDefaultClient()
	client.Subscribe("status_tasks/*?options=" + subOpt)
	nodeA.SubscriptionsBroker().Register(client)

	// Create pending task on Node B -> client receives create
	t1 := core.NewRecord(taskCol)
	t1.Set("title", "Fix bug")
	t1.Set("status", "pending")
	if err := nodeB.Save(t1); err != nil {
		t.Fatal(err)
	}
	expectClientMessage(t, client, "create", t1.Id, 5*time.Second)

	// Update t1 on Node B to 'completed' -> client must NOT receive update event!
	t1.Set("status", "completed")
	if err := nodeB.Save(t1); err != nil {
		t.Fatal(err)
	}
	expectNoClientMessage(t, client, 1*time.Second)
}
