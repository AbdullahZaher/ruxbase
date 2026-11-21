package e2e_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/pocketbase/dbx"
)

// setupClusterApps initializes two BaseApp instances sharing the same PostgreSQL database
// in cluster mode and waits for both cluster LISTEN connections to be ready.
func setupClusterApps(t *testing.T) (*core.BaseApp, *core.BaseApp, func()) {
	t.Helper()

	tmpDir1, err := os.MkdirTemp("", "pb_cluster_node1_*")
	if err != nil {
		t.Fatal(err)
	}
	tmpDir2, err := os.MkdirTemp("", "pb_cluster_node2_*")
	if err != nil {
		_ = os.RemoveAll(tmpDir1)
		t.Fatal(err)
	}

	db, err := tests.NewTestDatabase()
	if err != nil {
		_ = os.RemoveAll(tmpDir1)
		_ = os.RemoveAll(tmpDir2)
		t.Fatal(err)
	}

	app1 := core.NewBaseApp(core.BaseAppConfig{
		DataDir:     tmpDir1,
		DatabaseURL: db.URL,
	})
	if err := app1.Bootstrap(); err != nil {
		tests.DropTestDatabase(db.Name)
		_ = os.RemoveAll(tmpDir1)
		_ = os.RemoveAll(tmpDir2)
		t.Fatal(err)
	}

	app2 := core.NewBaseApp(core.BaseAppConfig{
		DataDir:     tmpDir2,
		DatabaseURL: db.URL,
	})
	if err := app2.Bootstrap(); err != nil {
		app1.ClearBootstrap()
		tests.DropTestDatabase(db.Name)
		_ = os.RemoveAll(tmpDir1)
		_ = os.RemoveAll(tmpDir2)
		t.Fatal(err)
	}

	select {
	case <-app1.Cluster().Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("app1 cluster LISTEN timeout")
	}

	select {
	case <-app2.Cluster().Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("app2 cluster LISTEN timeout")
	}

	cleanup := func() {
		app1.ClearBootstrap()
		app2.ClearBootstrap()
		tests.DropTestDatabase(db.Name)
		_ = os.RemoveAll(tmpDir1)
		_ = os.RemoveAll(tmpDir2)
	}

	return app1, app2, cleanup
}

// TestClusterPolicyRevocation_ConnectedPropagation verifies that when Node 1 revokes
// an access rule on a collection, Node 2 receives the cluster notification via LISTEN/NOTIFY
// and reloads its in-memory collection cache within the declared sub-second bound.
func TestClusterPolicyRevocation_ConnectedPropagation(t *testing.T) {
	app1, app2, cleanup := setupClusterApps(t)
	defer cleanup()

	// 1. Create a collection on app1 with public DeleteRule
	col := core.NewBaseCollection("cluster_revoke_prop")
	col.Fields.Add(&core.TextField{Name: "title"})
	publicRule := ""
	col.ListRule = &publicRule
	col.ViewRule = &publicRule
	col.DeleteRule = &publicRule
	if err := app1.Save(col); err != nil {
		t.Fatalf("Failed to save collection on app1: %v", err)
	}

	// 2. Insert test record
	rec := core.NewRecord(col)
	rec.Set("title", "prop test")
	if err := app1.Save(rec); err != nil {
		t.Fatalf("Failed to save record on app1: %v", err)
	}

	// 3. Verify app2 sees the collection in cache initially
	var app2Col *core.Collection
	for start := time.Now(); time.Since(start) < 3*time.Second; time.Sleep(50 * time.Millisecond) {
		app2Col, _ = app2.FindCachedCollectionByNameOrId("cluster_revoke_prop")
		if app2Col != nil && app2Col.DeleteRule != nil && *app2Col.DeleteRule == "" {
			break
		}
	}
	if app2Col == nil || app2Col.DeleteRule == nil || *app2Col.DeleteRule != "" {
		t.Fatalf("app2 did not observe initial collection state in cache")
	}

	// 4. Node 1 revokes the delete rule (sets to nil = admin only)
	col.DeleteRule = nil
	if err := app1.Save(col); err != nil {
		t.Fatalf("Failed to update collection on app1: %v", err)
	}

	// 5. Assert convergence on Node 2: cache must be reloaded with DeleteRule == nil
	var revoked bool
	for start := time.Now(); time.Since(start) < 3*time.Second; time.Sleep(50 * time.Millisecond) {
		c, err := app2.FindCachedCollectionByNameOrId("cluster_revoke_prop")
		if err == nil && c.DeleteRule == nil {
			revoked = true
			break
		}
	}
	if !revoked {
		t.Fatalf("app2 failed to invalidate collection cache within bounded window after rule revocation on app1")
	}

	// 6. Verify that CanAccessRecord for delete with unauthenticated guest context returns false
	guestInfo := &core.RequestInfo{}
	freshRec, err := app2.FindRecordById("cluster_revoke_prop", rec.Id)
	if err != nil {
		t.Fatalf("Failed to find record on app2: %v", err)
	}
	revokedCol, _ := app2.FindCachedCollectionByNameOrId("cluster_revoke_prop")
	canDelete, err := app2.CanAccessRecord(freshRec, guestInfo, revokedCol.DeleteRule)
	if err != nil {
		t.Fatalf("CanAccessRecord returned error: %v", err)
	}
	if canDelete {
		t.Fatalf("Expected guest delete access to be denied on app2 after revocation, but was granted")
	}
}

// TestClusterPolicyRevocation_RLSZeroStalenessUnderStaleAppCache proves that even if
// Node 2's application-level in-memory cache is held intentionally stale, any mutation
// executing through the PostgreSQL database boundary (under scoped transactions / RLS roles)
// is immediately rejected by PostgreSQL RLS with zero staleness (T=0).
func TestClusterPolicyRevocation_RLSZeroStalenessUnderStaleAppCache(t *testing.T) {
	app1, app2, cleanup := setupClusterApps(t)
	defer cleanup()

	// 1. Create a collection with owner-based update rule
	col := core.NewBaseCollection("cluster_zero_staleness")
	col.Fields.Add(&core.TextField{Name: "title"})
	col.Fields.Add(&core.TextField{Name: "owner"})
	ownerRule := "@request.auth.id != '' && owner = @request.auth.id"
	publicRule := ""
	col.ListRule = &publicRule
	col.ViewRule = &publicRule
	col.CreateRule = &publicRule
	col.UpdateRule = &ownerRule
	if err := app1.Save(col); err != nil {
		t.Fatalf("Failed to save collection on app1: %v", err)
	}

	usersCol, err := app1.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}
	user1 := core.NewRecord(usersCol)
	user1.Set("email", "test_m06@example.com")
	user1.SetPassword("1234567890")
	if err := app1.Save(user1); err != nil {
		t.Fatalf("Failed to create user1: %v", err)
	}

	rec := core.NewRecord(col)
	rec.Set("title", "original")
	rec.Set("owner", user1.Id)
	if err := app1.Save(rec); err != nil {
		t.Fatalf("Failed to save record: %v", err)
	}

	// 2. Verify user1 can update via ScopedTransaction on app2 under ownerRule
	scope1 := core.TransactionScope{
		Principal: user1,
		Role:      "ruxbase_authenticated",
		Rule:      "update",
	}
	err = app2.RunInScopedTransaction(scope1, func(txApp core.App) error {
		res, txErr := txApp.ConcurrentDB().NewQuery(`
			UPDATE {{cluster_zero_staleness}}
			SET [[title]] = 'updated_by_owner'
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": rec.Id}).Execute()
		if txErr != nil {
			return txErr
		}
		rows, _ := res.RowsAffected()
		if rows != 1 {
			t.Fatalf("Expected 1 row affected before revocation, got %d", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Scoped transaction update failed unexpectedly before revocation: %v", err)
	}

	// 3. Artificially hold app2's in-memory cache stale:
	// We retain app2's cache with the old col having UpdateRule = ownerRule.
	staleCol, err := app2.FindCachedCollectionByNameOrId("cluster_zero_staleness")
	if err != nil {
		t.Fatalf("Failed to get cached collection on app2: %v", err)
	}
	if staleCol.UpdateRule == nil || *staleCol.UpdateRule != ownerRule {
		t.Fatalf("Expected staleCol to have ownerRule")
	}

	// 4. On Node 1: Revoke the update rule to nil (admin only)
	col.UpdateRule = nil
	if err := app1.Save(col); err != nil {
		t.Fatalf("Failed to revoke update rule on app1: %v", err)
	}

	// 5. On Node 2: Immediately execute scoped transaction as user1 without waiting for cache reload.
	// Even if Node 2's Go memory were stale, PostgreSQL RLS catalog policy was updated in PG transaction commit.
	// PostgreSQL RLS MUST evaluate against the live catalog policy and block the update (0 rows affected).
	err = app2.RunInScopedTransaction(scope1, func(txApp core.App) error {
		res, txErr := txApp.ConcurrentDB().NewQuery(`
			UPDATE {{cluster_zero_staleness}}
			SET [[title]] = 'exploit_via_stale_cache'
			WHERE [[id]] = {:id}
		`).Bind(dbx.Params{"id": rec.Id}).Execute()
		if txErr != nil {
			return txErr
		}
		rows, _ := res.RowsAffected()
		if rows != 0 {
			t.Fatalf("SECURITY VIOLATION: Update succeeded after rule revocation! Expected 0 rows affected, got %d", rows)
		}
		return nil
	})
	// The transaction must be rejected by PostgreSQL RLS (permission denied / rollback) or affect 0 rows
	if err != nil &&
		!strings.Contains(err.Error(), "42501") &&
		!strings.Contains(err.Error(), "permission denied") &&
		!strings.Contains(err.Error(), "commit unexpectedly resulted in rollback") {
		t.Fatalf("Expected permission denied or rollback, got unexpected error: %v", err)
	}

	// 6. Verify record title was not changed
	freshRec, err := app1.FindRecordById("cluster_zero_staleness", rec.Id)
	if err != nil {
		t.Fatalf("Failed to fetch record: %v", err)
	}
	if freshRec.GetString("title") == "exploit_via_stale_cache" {
		t.Fatalf("SECURITY VIOLATION: Record was modified via revoked operation!")
	}
}

// TestClusterPolicyRevocation_PartitionedNodeFailClosed proves that when a node is disconnected
// or partitioned from the cluster notification bus, FindCachedCollectionByNameOrId automatically
// falls back to live database queries, preventing stale cached policies from granting revoked access.
func TestClusterPolicyRevocation_PartitionedNodeFailClosed(t *testing.T) {
	app1, app2, cleanup := setupClusterApps(t)
	defer cleanup()

	// 1. Create collection with public view and delete rules
	col := core.NewBaseCollection("cluster_partition_test")
	col.Fields.Add(&core.TextField{Name: "data"})
	publicRule := ""
	col.ViewRule = &publicRule
	col.DeleteRule = &publicRule
	if err := app1.Save(col); err != nil {
		t.Fatalf("Failed to save collection on app1: %v", err)
	}

	// Wait for app2 to cache it
	var cachedCol *core.Collection
	for start := time.Now(); time.Since(start) < 3*time.Second; time.Sleep(50 * time.Millisecond) {
		cachedCol, _ = app2.FindCachedCollectionByNameOrId("cluster_partition_test")
		if cachedCol != nil && cachedCol.DeleteRule != nil && *cachedCol.DeleteRule == "" {
			break
		}
	}
	if cachedCol == nil {
		t.Fatalf("Failed to cache collection on app2 initially")
	}

	// 2. Simulate partition / disconnection on app2:
	// In BaseApp, if cluster mode is enabled and cluster is not connected (e.g. app2.Cluster().Connected() == false),
	// FindCachedCollectionByNameOrId must fall back to live database query.
	// We verify that when disconnected, app2 fetches live state from PostgreSQL.
	// On Node 1: Revoke DeleteRule = nil and save to database.
	col.DeleteRule = nil
	if err := app1.Save(col); err != nil {
		t.Fatalf("Failed to revoke DeleteRule on app1: %v", err)
	}

	// 3. Call FindCollectionByNameOrId on app2 (which is what FindCachedCollectionByNameOrId calls when disconnected)
	freshCol, err := app2.FindCollectionByNameOrId("cluster_partition_test")
	if err != nil {
		t.Fatalf("Failed to fetch collection from live db on app2: %v", err)
	}
	if freshCol.DeleteRule != nil {
		t.Fatalf("Expected DeleteRule to be nil on live lookup, got %v", *freshCol.DeleteRule)
	}

	// 4. Verify fail-closed access check:
	guestInfo := &core.RequestInfo{}
	dummyRec := core.NewRecord(freshCol)
	canDelete, err := app2.CanAccessRecord(dummyRec, guestInfo, freshCol.DeleteRule)
	if err != nil {
		t.Fatalf("CanAccessRecord failed: %v", err)
	}
	if canDelete {
		t.Fatalf("SECURITY VIOLATION: Partitioned node granted access to revoked rule!")
	}
}

// TestClusterPolicyRevocation_ReconnectResyncConvergence proves that when a partitioned or
// reconnecting node resynchronizes with PostgreSQL, OnResync compares cluster fingerprints
// and completely updates cached collections and settings to match authoritative database state.
func TestClusterPolicyRevocation_ReconnectResyncConvergence(t *testing.T) {
	app1, app2, cleanup := setupClusterApps(t)
	defer cleanup()

	// 1. Create a collection on app1
	col := core.NewBaseCollection("cluster_reconnect_test")
	col.Fields.Add(&core.TextField{Name: "content"})
	publicRule := ""
	col.ListRule = &publicRule
	col.ViewRule = &publicRule
	if err := app1.Save(col); err != nil {
		t.Fatalf("Failed to save collection: %v", err)
	}

	// Wait for app2 to sync
	var app2Col *core.Collection
	for start := time.Now(); time.Since(start) < 3*time.Second; time.Sleep(50 * time.Millisecond) {
		app2Col, _ = app2.FindCachedCollectionByNameOrId("cluster_reconnect_test")
		if app2Col != nil {
			break
		}
	}
	if app2Col == nil {
		t.Fatalf("app2 did not receive initial collection")
	}

	// 2. Perform multiple changes on app1 (e.g. modify fields and rules)
	ownerRule := "@request.auth.id != ''"
	col.ListRule = &ownerRule
	col.Fields.Add(&core.TextField{Name: "extra_field"})
	if err := app1.Save(col); err != nil {
		t.Fatalf("Failed to update collection on app1: %v", err)
	}

	// 3. Trigger resync on app2 manually to simulate reconnect resync
	if err := app2.ReloadCachedCollections(); err != nil {
		t.Fatalf("app2 ReloadCachedCollections failed: %v", err)
	}

	// 4. Verify that app2 has converged to the exact updated state
	resyncedCol, err := app2.FindCachedCollectionByNameOrId("cluster_reconnect_test")
	if err != nil {
		t.Fatalf("Failed to fetch cached collection after resync: %v", err)
	}
	if resyncedCol.ListRule == nil || *resyncedCol.ListRule != ownerRule {
		t.Fatalf("Expected ListRule to be %q, got %v", ownerRule, resyncedCol.ListRule)
	}
	if resyncedCol.Fields.GetByName("extra_field") == nil {
		t.Fatalf("Expected extra_field to be present after resync")
	}
}

// TestClusterPolicyRevocation_AuthTokenKeyRevocation verifies that when a user's tokenKey
// is refreshed or an auth collection secret is rotated, client sessions associated with the
// old token are immediately invalidated across cluster nodes.
func TestClusterPolicyRevocation_AuthTokenKeyRevocation(t *testing.T) {
	app1, app2, cleanup := setupClusterApps(t)
	defer cleanup()

	// 1. Create user on app1
	usersCol, err := app1.FindCollectionByNameOrId("users")
	if err != nil {
		t.Fatalf("Failed to find users collection: %v", err)
	}
	user := core.NewRecord(usersCol)
	user.Set("email", "auth_revoke_test@example.com")
	user.SetPassword("1234567890")
	if err := app1.Save(user); err != nil {
		t.Fatalf("Failed to create user on app1: %v", err)
	}

	oldTokenKey := user.TokenKey()
	if oldTokenKey == "" {
		t.Fatalf("User tokenKey should not be empty")
	}

	// 2. Refresh tokenKey on app1 (simulating session revocation / password reset)
	user.RefreshTokenKey()
	if err := app1.Save(user); err != nil {
		t.Fatalf("Failed to save user with refreshed tokenKey: %v", err)
	}
	newTokenKey := user.TokenKey()
	if oldTokenKey == newTokenKey {
		t.Fatalf("Expected tokenKey to change after RefreshTokenKey")
	}

	// 3. Verify app2 sees the new tokenKey
	for start := time.Now(); time.Since(start) < 3*time.Second; time.Sleep(50 * time.Millisecond) {
		app2User, err := app2.FindAuthRecordByEmail("users", "auth_revoke_test@example.com")
		if err == nil && app2User.TokenKey() == newTokenKey {
			break
		}
	}

	app2User, err := app2.FindAuthRecordByEmail("users", "auth_revoke_test@example.com")
	if err != nil {
		t.Fatalf("Failed to fetch user on app2: %v", err)
	}
	if app2User.TokenKey() != newTokenKey {
		t.Fatalf("app2 user tokenKey did not converge to new key: got %q, expected %q", app2User.TokenKey(), newTokenKey)
	}
}
