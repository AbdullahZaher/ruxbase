package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestPostgreSQL_TelemetryCollectorSeparatesSlotsAndStandbys(t *testing.T) {
	if os.Getenv(tests.TestDatabaseURLEnv) == "" {
		t.Skip("RUXBASE_TEST_URL not configured")
	}

	app, err := tests.NewEmptyTestApp()
	if err != nil {
		t.Fatalf("NewEmptyTestApp: %v", err)
	}
	defer app.Cleanup()

	suCol, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatal(err)
	}
	su := core.NewRecord(suCol)
	su.SetEmail("telemetry-test@staging.invalid")
	su.SetPassword("TelemetryPass!2026")
	if err := app.Save(su); err != nil {
		t.Fatal(err)
	}
	tok, err := su.NewAuthToken()
	if err != nil {
		t.Fatal(err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatal(err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	col, err := NewCollector(ctx, &http.Client{Timeout: 5 * time.Second}, srv.URL, tok, app.DatabaseURL(), t.TempDir(), time.Now().UTC())
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	defer col.Close()

	sample := col.Collect(ctx)
	if !sample.HealthReadyOK {
		t.Fatalf("expected HealthReadyOK=true, got status=%d err=%q", sample.HealthReadyStatus, sample.CollectorError)
	}
	if sample.RuxbaseOps == nil || sample.RuxbaseOps.Runtime.LiveHeapBytes == 0 {
		t.Fatalf("expected LiveHeapBytes > 0 in RuxbaseOps, got %+v", sample.RuxbaseOps)
	}
	if sample.Postgres == nil {
		t.Fatalf("expected non-nil Postgres metrics, err=%q", sample.CollectorError)
	}
	if len(sample.Postgres.QueryErrors) > 0 {
		t.Fatalf("unexpected PostgreSQL query errors: %v", sample.Postgres.QueryErrors)
	}
	if sample.Postgres.StandbyStatus != StatusNotConfigured {
		t.Fatalf("expected StandbyStatus=%s without physical standbys, got %s", StatusNotConfigured, sample.Postgres.StandbyStatus)
	}
}

func TestClassifyReplicationEntryDistinguishesLogicalAndStandby(t *testing.T) {
	pg := &PostgresMetrics{
		LogicalWALStatus: StatusNotConfigured,
		StandbyStatus:    StatusNotConfigured,
	}
	// Unbound startup/teardown walsender on current DB must never become a physical standby.
	classifyReplicationEntry(pg, "", "", "ruxbase_staging", "ruxbase_wal", "ruxbase-realtime", "startup", "async", 0)
	// Bound logical slot consumer.
	classifyReplicationEntry(pg, "ruxbase_slot_1", "logical", "ruxbase_staging", "ruxbase_wal", "ruxbase-realtime", "streaming", "async", 0)
	if len(pg.LogicalWALConsumers) != 2 || len(pg.StandbyReplicas) != 0 {
		t.Fatalf("expected 2 logical consumers and 0 standbys, got logical=%d standby=%d", len(pg.LogicalWALConsumers), len(pg.StandbyReplicas))
	}
	// Database-less physical standby replica.
	classifyReplicationEntry(pg, "standby_phys_1", "physical", "", "replicator", "pg_standby_1", "streaming", "potential", 0.12)
	if len(pg.StandbyReplicas) != 1 || pg.StandbyReplicas[0].ApplicationName != "pg_standby_1" {
		t.Fatalf("expected 1 physical standby, got %+v", pg.StandbyReplicas)
	}
}
