package journeys

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
)

func TestRunnerDetectsCrossTenantLeakAndRecordsIntentAckLedger(t *testing.T) {
	leakMode := false
	var recCounter atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/records"):
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["title"] == "FORGED_CROSS_TENANT_SOAK" {
				if leakMode {
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"id":"forged1"}`))
					return
				}
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"forbidden"}`))
				return
			}
			id := fmt.Sprintf("rec_%010d", recCounter.Add(1))
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        id,
				"tenant_id": body["tenant_id"],
			})
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/records/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"updated"}`))
		case r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/records/"):
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/records/foreign_seed_1"):
			if leakMode {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"id":"foreign_seed_1","tenant_id":"soak_tenant_01"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"items":[]}`))
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	st, err := state.Open(dir, "W11-05-UNIT", "pilot", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	fx := &Fixtures{
		Users: []SyntheticUser{
			{TenantID: "soak_tenant_00", UserID: "u0", Token: "tok0"},
			{TenantID: "soak_tenant_01", UserID: "u1", Token: "tok1"},
		},
		SeedIDsByTen: map[string]string{
			"soak_tenant_00": "seed_0",
			"soak_tenant_01": "foreign_seed_1",
		},
		VectorDims: 128,
	}
	r := NewRunner(srv.Client(), srv.URL, fx, st)

	// 1. Clean cross-tenant denial
	out := r.runCrossTenantNegative(context.Background(), fx.Users[0], "soak_tenant_01", "foreign_seed_1")
	if out.CrossTenantLk || out.AppFailure || !out.CrossTenantChk || out.HTTPAttempted != 2 || out.HTTPSucceeded != 2 {
		t.Fatalf("expected clean cross-tenant denial with 2 HTTP requests, got %+v", out)
	}

	// 2. Simulated cross-tenant leak MUST be flagged immediately
	leakMode = true
	outLeak := r.runCrossTenantNegative(context.Background(), fx.Users[0], "soak_tenant_01", "foreign_seed_1")
	if !outLeak.CrossTenantLk || !outLeak.IntegrityErr {
		t.Fatalf("expected CrossTenantLk=true on unauthorized exposure, got %+v", outLeak)
	}

	// 3. Delete on empty pool must NOT fake 204 NoContent no-op; performs real list read and marks NoOpSkipped
	leakMode = false
	rng := rand.New(rand.NewPCG(7, 11))
	delEmpty := r.runCRUDDelete(context.Background(), fx.Users[0], rng)
	if delEmpty.NoOpSkipped != 1 || delEmpty.Kind != OpCRUDRead || delEmpty.HTTPAttempted != 1 {
		t.Fatalf("expected empty delete fallback to real read with NoOpSkipped=1, got %+v", delEmpty)
	}

	// 4. Create -> Update -> Delete lifecycle records intent+ack entries for 100% of mutations
	oCreate := r.runCRUDCreate(context.Background(), fx.Users[0], rng)
	oUpdate := r.runCRUDUpdate(context.Background(), fx.Users[0], rng)
	oDelete := r.runCRUDDelete(context.Background(), fx.Users[0], rng)
	for _, o := range []OpOutcome{oCreate, oUpdate, oDelete} {
		if o.AppFailure || o.GenFailure || o.MutationsCommitted != 1 {
			t.Fatalf("unexpected lifecycle outcome: %+v", o)
		}
	}
	entries, err := state.ReadLedger(dir)
	if err != nil || len(entries) != 6 { // 3 mutations * (intent + ack) = 6 entries
		t.Fatalf("expected 6 intent/ack ledger entries, got len=%d err=%v", len(entries), err)
	}

	// 5. Concurrent race test across create/update/delete/read
	var wg sync.WaitGroup
	for w := range 6 {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			localRng := rand.New(rand.NewPCG(uint64(id+1), 99))
			for range 15 {
				_ = r.ExecuteMixed(context.Background(), localRng, 0.4)
			}
		}(w)
	}
	wg.Wait()

	// 6. Ledger persistence failure MUST fail closed and surface LedgerPersistErr
	_ = os.Remove(filepath.Join(dir, "ledger.jsonl"))
	if err := os.Mkdir(filepath.Join(dir, "ledger.jsonl"), 0o750); err != nil {
		t.Fatal(err)
	}
	failOut := r.runCRUDCreate(context.Background(), fx.Users[0], rng)
	if !failOut.LedgerPersistErr || !failOut.IntegrityErr || failOut.MutationsCommitted != 0 {
		t.Fatalf("expected LedgerPersistErr & aborted mutation when ledger fails, got %+v", failOut)
	}
}

func TestBoundedIDSetNeverGrowsUnbounded(t *testing.T) {
	s := newBoundedIDSet(100)
	for i := range 5000 {
		s.add(fmt.Sprintf("id-%d", i))
	}
	if len(s.items) > 100 {
		t.Fatalf("boundedIDSet leaked beyond cap 100: len=%d", len(s.items))
	}
	if !s.add("id-0") {
		t.Fatal("expected evicted old ID id-0 to be re-addable after eviction")
	}
}
