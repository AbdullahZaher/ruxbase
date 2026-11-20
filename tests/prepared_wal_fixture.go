package tests

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core/cluster"
	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/pocketbase/dbx"
)

// StartPreparedWALFixture starts the existing decoder on a guarded runtime
// event-store pool and a different CI WAL login. All provisioning is confined
// to the disposable fixture. It does not enable realtime on the HTTP server.
func StartPreparedWALFixture(t *testing.T, owner *sql.DB, runtimeURL string, identities pgrolecheck.IdentityContract, collectionID string) (verify func(string), stop func()) {
	t.Helper()
	raw := os.Getenv("RUXBASE_TEST_WAL_URL")
	if raw == "" || identities.Replication != "ruxbase_ci_wal" {
		t.Fatal("dedicated disposable WAL configuration required")
	}
	var database string
	if err := owner.QueryRow("SELECT current_database()").Scan(&database); err != nil || !strings.HasPrefix(database, TestDatabasePrefix) {
		t.Fatal("prepared WAL fixture requires a disposable database")
	}
	u, err := url.Parse(raw)
	if err != nil || u.User == nil || u.User.Username() != identities.Replication {
		t.Fatal("invalid disposable WAL URL")
	}
	u.Path = "/" + database
	dedicated := cluster.DedicatedReplication{DatabaseURL: u.String(), Role: identities.Replication}
	wal, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal("unable to open disposable WAL login")
	}
	var cleanup []func()
	cleanup = append(cleanup, func() { _ = wal.Close() })
	complete := false
	defer func() {
		if !complete {
			for i := len(cleanup) - 1; i >= 0; i-- {
				cleanup[i]()
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	if err := identities.CheckSeparation(ctx, wal, identities.Replication); err != nil {
		t.Fatal(err)
	}

	// Offline migration principal owns the collection and publication.
	if _, err := owner.ExecContext(ctx, "ALTER TABLE public.m01_fixture_items REPLICA IDENTITY FULL"); err != nil {
		t.Fatal("unable to prepare disposable replica identity")
	}
	if _, err := owner.ExecContext(ctx, "CREATE PUBLICATION ruxbase_rt_pub FOR TABLE public.m01_fixture_items"); err != nil {
		t.Fatal("unable to prepare disposable publication")
	}
	slot := cluster.SlotName(database)
	if _, err := wal.ExecContext(ctx, "SELECT pg_catalog.pg_create_logical_replication_slot($1, 'pgoutput', false, false, true)", slot); err != nil {
		t.Fatalf("unable to prepare disposable logical slot: %v", err)
	}
	cleanup = append(cleanup, func() {
		// The consumer is stopped before this cleanup; only its fixture slot
		// is removed by the explicitly provisioned WAL principal.
		dropCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := wal.ExecContext(dropCtx, "SELECT pg_catalog.pg_drop_replication_slot($1)", slot); err != nil {
			t.Error("unable to remove disposable logical slot")
		}
	})
	if _, err := owner.ExecContext(ctx, "INSERT INTO public._params (id, value) VALUES ('ruxbaseRealtimeSlot', $1)", slot); err != nil {
		t.Fatal("unable to record disposable slot metadata")
	}
	db, err := pgrolecheck.OpenGuardedPool(runtimeURL, identities.Runtime, "public")
	if err != nil {
		t.Fatal(err)
	}
	cleanup = append(cleanup, func() { _ = db.Close() })
	builder := dbx.NewFromDB(db, "pgx")
	if _, err := identities.AdmitRuntime(ctx, db); err != nil {
		t.Fatal(err)
	}
	secret := security.RandomString(50)
	worker := cluster.New(cluster.Config{
		DSN:    func() string { return runtimeURL },
		DB:     func() dbx.Builder { return builder },
		Secret: func() (string, error) { return secret, nil },
	})
	if err := worker.CheckConfiguredReplicationSupport(ctx, dedicated); err != nil {
		t.Fatal(err)
	}
	worker.SetRealtimeSource(cluster.RealtimeSourceReplication)
	worker.StartReplication(cluster.ReplicationConfig{
		Dedicated:        dedicated,
		PreparedSlotOnly: true,
		PrepareTables: func() error {
			var ready bool
			err := builder.NewQuery(`
SELECT count(*) = 1 AND bool_and(schemaname = 'public'
    AND tablename = 'm01_fixture_items' AND rowfilter IS NULL)
FROM pg_catalog.pg_publication_tables WHERE pubname = 'ruxbase_rt_pub'
`).Row(&ready)
			if err != nil || !ready {
				return errors.New("disposable publication mismatch")
			}
			return nil
		},
		ResolveCollection: func(table string) string {
			if table == "m01_fixture_items" {
				return collectionID
			}
			return table
		},
	})
	cleanup = append(cleanup, worker.StopReplication)
	for {
		var active bool
		if err := owner.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_catalog.pg_replication_slots s
    JOIN pg_catalog.pg_stat_activity a ON a.pid = s.active_pid
    WHERE s.slot_name = $1 AND s.database = current_database()
      AND a.usename = $2
)
`, slot, identities.Replication).Scan(&active); err != nil {
			t.Fatal("unable to inspect disposable slot")
		}
		if active {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("dedicated WAL consumer did not become active")
		case <-time.After(100 * time.Millisecond):
		}
	}
	complete = true
	return func(recordID string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for {
			var total int
			if err := owner.QueryRowContext(ctx, "SELECT count(*) FROM public._realtime_events WHERE source = 'replication' AND collection_id = $1 AND record_id = $2", collectionID, recordID).Scan(&total); err != nil {
				t.Fatal("unable to inspect persisted fixture event")
			}
			if total == 1 {
				t.Log("M01_COMBINED_WAL: restricted HTTP create decoded by dedicated WAL session and persisted")
				return
			}
			if total > 1 {
				t.Fatal("duplicate persisted fixture events")
			}
			select {
			case <-ctx.Done():
				t.Fatal("HTTP write was not decoded by dedicated WAL consumer")
			case <-time.After(100 * time.Millisecond):
			}
		}
	}, func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
	}
}
