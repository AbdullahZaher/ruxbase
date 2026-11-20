package core_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/AbdullahZaher/ruxbase/tools/store"
	"github.com/pocketbase/dbx"
)

func TestNotifyWatcher_SettingsUpdate(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "pb_notify_test*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// ruxbase: both instances share the same database
	db, err := tests.NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(db.Name)

	app1 := core.NewBaseApp(core.BaseAppConfig{
		DataDir:     tmpDir,
		DatabaseURL: db.URL,
	})
	if err := app1.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	app2 := core.NewBaseApp(core.BaseAppConfig{
		DataDir:     tmpDir,
		DatabaseURL: db.URL,
	})
	if err := app2.Bootstrap(); err != nil {
		t.Fatal(err)
	}

	defer app1.ClearBootstrap()
	defer app2.ClearBootstrap()

	select {
	case <-app2.Cluster().Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("app2 cluster LISTEN timeout")
	}

	// A local save reloads settings synchronously, while cluster resync can
	// independently reload the same state. LISTEN readiness does not imply
	// that the initial asynchronous resync has completed. Assert convergence
	// to the saved value, not exactly-once invocation of a distributed hook.
	// Direct ReloadSettings hook counts are covered in settings_query_test.go.
	observe := func(app *core.BaseApp) <-chan struct{} {
		done := make(chan struct{}, 1)
		app.OnSettingsReload().BindFunc(func(e *core.SettingsReloadEvent) error {
			if err := e.Next(); err != nil {
				return err
			}
			settings, err := e.App.Settings().Clone()
			if err != nil {
				return err
			}
			if len(settings.SuperuserIPs) == 1 && settings.SuperuserIPs[0] == "127.0.0.1" {
				// Never block a background reload on a completed test.
				select {
				case done <- struct{}{}:
				default:
				}
			}
			return nil
		})
		return done
	}

	local := observe(app1)
	remote := observe(app2)

	settings, err := app1.Settings().Clone()
	if err != nil {
		t.Fatal(err)
	}
	settings.SuperuserIPs = []string{"127.0.0.1"}
	if err := app1.Save(settings); err != nil {
		t.Fatal(err)
	}

	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	for _, event := range []struct {
		name string
		done <-chan struct{}
	}{
		{"app1", local},
		{"app2", remote},
	} {
		select {
		case <-timeout.C:
			t.Fatalf("%s did not apply the saved settings", event.name)
		case <-event.done:
		}
	}

	for _, app := range []*core.BaseApp{app1, app2} {
		snapshot, err := app.Settings().Clone()
		if err != nil {
			t.Fatal(err)
		}
		if len(snapshot.SuperuserIPs) != 1 || snapshot.SuperuserIPs[0] != "127.0.0.1" {
			t.Fatalf("Expected exactly 127.0.0.1 in the converged settings, got %v", snapshot.SuperuserIPs)
		}
	}
}

func TestNotifyWatcher_CollectionsUpdate(t *testing.T) {
	t.Parallel()

	tmpDir, err := os.MkdirTemp("", "pb_notify_test*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// ruxbase: both instances share the same database
	db, err := tests.NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(db.Name)

	app1 := core.NewBaseApp(core.BaseAppConfig{
		DataDir:     tmpDir,
		DatabaseURL: db.URL,
	})
	if err := app1.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app1.ClearBootstrap()

	app2 := core.NewBaseApp(core.BaseAppConfig{
		DataDir:     tmpDir,
		DatabaseURL: db.URL,
	})
	if err := app2.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app2.ClearBootstrap()

	select {
	case <-app2.Cluster().Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("app2 cluster LISTEN timeout")
	}

	// ruxbase: the concurrent and nonconcurrent builders share the same pool
	testQueries := store.New[string, []string](nil)
	logQuery := func(sql string) {
		if strings.Contains(sql, `FROM "_collections"`) {
			testQueries.SetFunc("collections", func(old []string) []string {
				return append(old, sql)
			})
		}
	}
	app2.ConcurrentDB().(*dbx.DB).QueryLogFunc = func(ctx context.Context, t time.Duration, sql string, rows *sql.Rows, err error) {
		logQuery(sql)
	}
	app2.ConcurrentDB().(*dbx.DB).ExecLogFunc = func(ctx context.Context, t time.Duration, sql string, result sql.Result, err error) {
		logQuery(sql)
	}

	// create/update/delete app1 collections should trigger an immediate reload in app2
	// (the changes received while reloading are coalesced into a single extra reload)
	dummyCollection := core.NewBaseCollection("test")
	if err := app1.Save(dummyCollection); err != nil {
		t.Fatal(err)
	}
	dummyCollection.Fields.Add(&core.TextField{Name: "test"})
	if err := app1.Save(dummyCollection); err != nil {
		t.Fatal(err)
	}
	if err := app1.Delete(dummyCollection); err != nil {
		t.Fatal(err)
	}

	// currently there is no hook for the collections cache reload so we poll instead
	deadline := time.Now().Add(1 * time.Second)
	for len(testQueries.Get("collections")) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}

	// wait a little longer to ensure that there are no other reloads
	time.Sleep(200 * time.Millisecond)

	// ruxbase: at most one reload per change
	collectionsQueries := testQueries.Get("collections")
	if len(collectionsQueries) < 1 || len(collectionsQueries) > 3 {
		t.Fatalf("Expected 1-3 collections queries, got %d (%v)", len(collectionsQueries), collectionsQueries)
	}

	expectedQuery := `SELECT {{_collections}}.* FROM "_collections" ORDER BY "created" ASC, "id" ASC`
	for _, q := range collectionsQueries {
		if q != expectedQuery {
			t.Fatalf("Expected query\n%s\ngot\n%s", expectedQuery, q)
		}
	}

	if _, err := app2.FindCachedCollectionByNameOrId("test"); err == nil {
		t.Fatal("Expected the deleted collection to be removed from the app2 cache")
	}

	// a collection created in app1 should be visible in app2
	visible := core.NewBaseCollection("test_visible")
	if err := app1.Save(visible); err != nil {
		t.Fatal(err)
	}

	deadline = time.Now().Add(1 * time.Second)
	for {
		if _, err := app2.FindCachedCollectionByNameOrId("test_visible"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Expected the new app1 collection to be cached in app2 in less than 1s")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestFindCachedCollectionClusterFallback(t *testing.T) {
	t.Parallel()

	db, err := tests.NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(db.Name)

	newApp := func(clusterMode bool) *core.BaseApp {
		app := core.NewBaseApp(core.BaseAppConfig{DataDir: t.TempDir(), DatabaseURL: db.URL, ClusterMode: clusterMode})
		if err := app.Bootstrap(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { app.ClearBootstrap() })
		return app
	}

	app1 := newApp(true)
	app2 := newApp(true)
	app3 := newApp(false)

	// stop the listeners so that the caches are not reloaded
	app2.Cluster().Stop()
	app3.Cluster().Stop()

	if err := app1.Save(core.NewBaseCollection("fallback_test")); err != nil {
		t.Fatal(err)
	}

	// cluster mode: loaded from the database
	if _, err := app2.FindCachedCollectionByNameOrId("FALLBACK_test"); err != nil {
		t.Fatalf("Expected the collection to be found in cluster mode, got %v", err)
	}

	// single node: the cache only
	if _, err := app3.FindCachedCollectionByNameOrId("fallback_test"); err == nil {
		t.Fatal("Expected the collection to be missing from the stale cache without cluster mode")
	}
}

// ruxbase: a change without a cluster message (eg. a lost message)
// is loaded by the resync of the other nodes.
func TestNotifyWatcher_ResyncLostChange(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	db, err := tests.NewTestDatabase()
	if err != nil {
		t.Fatal(err)
	}
	defer tests.DropTestDatabase(db.Name)

	app1 := core.NewBaseApp(core.BaseAppConfig{DataDir: tmpDir, DatabaseURL: db.URL})
	if err := app1.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app1.ClearBootstrap()

	app2 := core.NewBaseApp(core.BaseAppConfig{DataDir: tmpDir, DatabaseURL: db.URL})
	if err := app2.Bootstrap(); err != nil {
		t.Fatal(err)
	}
	defer app2.ClearBootstrap()

	select {
	case <-app2.Cluster().Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("app2 cluster LISTEN timeout")
	}

	_, err = app1.DB().NewQuery("UPDATE {{_collections}} SET [[listRule]] = {:rule} WHERE [[name]] = 'users'").
		Bind(dbx.Params{"rule": "id = 'resync'"}).
		Execute()
	if err != nil {
		t.Fatal(err)
	}

	app2.Cluster().Resync()

	deadline := time.Now().Add(5 * time.Second)
	for {
		users, err := app2.FindCachedCollectionByNameOrId("users")
		if err != nil {
			t.Fatal(err)
		}
		if users.ListRule != nil && *users.ListRule == "id = 'resync'" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Expected the resynced list rule, got %v", users.ListRule)
		}
		time.Sleep(50 * time.Millisecond)
	}
}
