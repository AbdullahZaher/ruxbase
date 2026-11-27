package verifier

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/apis"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/journeys"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgreSQL_SoakRLSVerifierLifecycleAndFailures validates the full W11-05
// workload + intent/ack ledger + RLS-aware verifier + failure-mode classifications
// against a real disposable PostgreSQL 17/18 database.
func TestPostgreSQL_SoakRLSVerifierLifecycleAndFailures(t *testing.T) {
	if os.Getenv(tests.TestDatabaseURLEnv) == "" {
		t.Skip("RUXBASE_TEST_URL not configured")
	}
	app, err := tests.NewEmptyTestApp()
	if err != nil {
		t.Fatalf("failed creating disposable test app: %v", err)
	}
	defer app.Cleanup()

	// Ensure a staging superuser exists in the disposable test app
	suCol, err := app.FindCollectionByNameOrId(core.CollectionNameSuperusers)
	if err != nil {
		t.Fatalf("find _superusers: %v", err)
	}
	su := core.NewRecord(suCol)
	su.SetEmail("soak-pg-test@staging.invalid")
	su.SetPassword("SoakSuperTestPass!2026")
	if err := app.Save(su); err != nil {
		t.Fatalf("save superuser: %v", err)
	}

	router, err := apis.NewRouter(app)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	mux, err := router.BuildMux()
	if err != nil {
		t.Fatalf("BuildMux: %v", err)
	}
	srv := httptest.NewServer(mux)
	defer srv.Close()

	ctx := context.Background()
	client := journeys.NewHTTPClient(15 * time.Second)
	fx, err := journeys.EnsureFixtures(ctx, client, srv.URL, "soak-pg-test@staging.invalid", "SoakSuperTestPass!2026", 3)
	if err != nil {
		t.Fatalf("EnsureFixtures: %v", err)
	}

	campDir := t.TempDir()
	const cid = "W11-05-PG-INTEGRATION"
	store, err := state.Open(campDir, cid, "pilot", time.Hour)
	if err != nil {
		t.Fatalf("state.Open: %v", err)
	}
	if err := state.WriteJSON(filepath.Join(campDir, "worker-heartbeat.json"), state.WorkerHeartbeat{
		CampaignID:       cid,
		PID:              1,
		StartedAtUTC:     time.Now().UTC().Format(time.RFC3339),
		LastHeartbeatUTC: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	// Run concurrent mixed journeys (create, update, delete, read, negative cross-tenant)
	runner := journeys.NewRunner(client, srv.URL, fx, store)
	var wg sync.WaitGroup
	for w := range 4 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			rng := rand.New(rand.NewPCG(uint64(id+10), 42))
			for range 12 {
				out := runner.ExecuteMixed(ctx, rng, 0.45)
				if out.CrossTenantLk || out.LedgerPersistErr {
					t.Errorf("unexpected critical violation during journey: %+v", out)
				}
			}
		}(w)
	}
	wg.Wait()

	// Open real pgxpool using the non-superuser disposable test role (same as ruxbase_app)
	pool, err := pgxpool.New(ctx, app.DatabaseURL())
	if err != nil {
		t.Fatalf("open pgxpool: %v", err)
	}
	defer pool.Close()

	// 1. Happy path: owner-visible + HTTP tenant + Strict-RLS SQL verification must PASS cleanly
	rep := RunIntegrityCheck(ctx, CheckOptions{
		Client:        client,
		BaseURL:       srv.URL,
		SuperToken:    fx.SuperToken(),
		Fixtures:      fx,
		Pool:          pool,
		EvidenceDir:   campDir,
		CampaignID:    cid,
		FullReconcile: true,
	})
	if !rep.Passed || rep.VerificationIncomplete {
		t.Fatalf("expected clean integrity verification PASS on real PG, got %+v", rep)
	}
	if rep.ConfirmedMissingRows != 0 || rep.CorruptedChecksumRows != 0 || !rep.RLSEnabled || !rep.WorkerCampaignMatch {
		t.Fatalf("unexpected verification counters: %+v", rep)
	}
	if rep.HTTPVerifiedRows == 0 || rep.HTTPCrossTenantDenied == 0 {
		t.Fatalf("expected live HTTP & cross-tenant RLS checks to execute, got %+v", rep)
	}

	// 2. Simulate restricted non-owner monitoring role under RLS -> MUST report HIDDEN_BY_RLS, NEVER ConfirmedMissingRows
	monRole := fmt.Sprintf("ruxbase_t_mon_%d", time.Now().UnixNano()%1000000)
	if _, err := pool.Exec(ctx, fmt.Sprintf("CREATE ROLE %s LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD 'monpass'", monRole)); err != nil {
		t.Fatalf("create temp non-owner monitor role: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, fmt.Sprintf("REVOKE ALL ON ALL TABLES IN SCHEMA public FROM %s", monRole))
		_, _ = pool.Exec(ctx, fmt.Sprintf("REVOKE USAGE ON SCHEMA public FROM %s", monRole))
		_, _ = pool.Exec(ctx, fmt.Sprintf("DROP ROLE IF EXISTS %s", monRole))
	}()
	_, _ = pool.Exec(ctx, fmt.Sprintf("GRANT USAGE ON SCHEMA public TO %s; GRANT SELECT ON soak_items, _jobs, _outbox_events TO %s;", monRole, monRole))

	monCfg := pool.Config().Copy()
	monCfg.ConnConfig.User = monRole
	monCfg.ConnConfig.Password = "monpass"
	monPool, err := pgxpool.NewWithConfig(ctx, monCfg)
	if err != nil {
		t.Fatalf("open non-owner monitor pool: %v", err)
	}
	defer monPool.Close()

	repMon := RunIntegrityCheck(ctx, CheckOptions{
		Client:        client,
		BaseURL:       srv.URL,
		SuperToken:    fx.SuperToken(),
		Pool:          monPool,
		EvidenceDir:   campDir,
		CampaignID:    cid,
		FullReconcile: true,
	})
	if repMon.Passed || !repMon.VerificationIncomplete || repMon.DirectSQLVisibility != "HIDDEN_BY_RLS" {
		t.Fatalf("expected HIDDEN_BY_RLS + VerificationIncomplete on restricted monitor role, got %+v", repMon)
	}
	if repMon.ConfirmedMissingRows != 0 || repMon.HiddenByRLS == 0 {
		t.Fatalf("monitor role must classify RLS-masked rows as HiddenByRLS (not ConfirmedMissingRows), got %+v", repMon)
	}

	// 3. Permission denied (SQLSTATE 42501) -> PermissionErrors > 0, VerificationIncomplete = true
	_, _ = pool.Exec(ctx, fmt.Sprintf("REVOKE SELECT ON soak_items FROM %s;", monRole))
	repPerm := RunIntegrityCheck(ctx, CheckOptions{
		Client:        client,
		BaseURL:       srv.URL,
		SuperToken:    fx.SuperToken(),
		Pool:          monPool,
		EvidenceDir:   campDir,
		CampaignID:    cid,
		FullReconcile: true,
	})
	if repPerm.Passed || !repPerm.VerificationIncomplete || repPerm.PermissionErrors == 0 {
		t.Fatalf("expected NO_SELECT_PRIVILEGE / PermissionErrors > 0, got %+v", repPerm)
	}

	// 4. Actual checksum mismatch in PostgreSQL -> CorruptedChecksumRows == 1, Passed == false
	var sampleID, origChecksum string
	if err := pool.QueryRow(ctx, "SELECT id, checksum FROM soak_items WHERE op_id LIKE 'op-%' OR op_id LIKE 'upd-%' LIMIT 1").Scan(&sampleID, &origChecksum); err == nil && sampleID != "" {
		_, _ = pool.Exec(ctx, "UPDATE soak_items SET checksum = 'deadbeefcorrupted' WHERE id = $1", sampleID)
		repCorrupt := RunIntegrityCheck(ctx, CheckOptions{
			Client:        client,
			BaseURL:       srv.URL,
			SuperToken:    fx.SuperToken(),
			Pool:          pool,
			EvidenceDir:   campDir,
			CampaignID:    cid,
			FullReconcile: true,
		})
		if repCorrupt.Passed || repCorrupt.CorruptedChecksumRows == 0 {
			t.Fatalf("expected checksum mismatch failure, got %+v", repCorrupt)
		}
		// Restore checksum and test actual confirmed missing row
		_, _ = pool.Exec(ctx, "DELETE FROM soak_items WHERE id = $1", sampleID)
		repMissing := RunIntegrityCheck(ctx, CheckOptions{
			Client:        client,
			BaseURL:       srv.URL,
			SuperToken:    fx.SuperToken(),
			Pool:          pool,
			EvidenceDir:   campDir,
			CampaignID:    cid,
			FullReconcile: true,
		})
		if repMissing.Passed || repMissing.ConfirmedMissingRows == 0 {
			t.Fatalf("expected ConfirmedMissingRows > 0 when committed row deleted directly in PG, got %+v", repMissing)
		}
	}

	// 5. Connection pool failure -> ConnectionErrors > 0, VerificationIncomplete = true
	closedPool, _ := pgxpool.New(ctx, app.DatabaseURL())
	closedPool.Close()
	repConn := RunIntegrityCheck(ctx, CheckOptions{
		Client:        client,
		BaseURL:       srv.URL,
		SuperToken:    fx.SuperToken(),
		Pool:          closedPool,
		EvidenceDir:   campDir,
		CampaignID:    cid,
		FullReconcile: true,
	})
	if repConn.Passed || !repConn.VerificationIncomplete || (repConn.ConnectionErrors == 0 && repConn.QueryErrors == 0) {
		t.Fatalf("expected closed-pool connection/query error classification, got %+v", repConn)
	}
}
