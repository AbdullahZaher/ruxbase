package journeys

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
)

func TestRunnerDetectsCrossTenantLeakAndRecordsLedger(t *testing.T) {
	leakMode := false
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
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "rec_1234567890",
				"tenant_id": body["tenant_id"],
			})
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
	if out.CrossTenantLk || out.AppFailure || !out.CrossTenantChk {
		t.Fatalf("expected clean cross-tenant denial, got %+v", out)
	}

	// 2. Simulated cross-tenant leak MUST be flagged immediately
	leakMode = true
	outLeak := r.runCrossTenantNegative(context.Background(), fx.Users[0], "soak_tenant_01", "foreign_seed_1")
	if !outLeak.CrossTenantLk || !outLeak.IntegrityErr {
		t.Fatalf("expected CrossTenantLk=true on unauthorized exposure, got %+v", outLeak)
	}

	// 3. Create records and confirm durable ledger recording
	leakMode = false
	rng := rand.New(rand.NewPCG(7, 11))
	for range 10 {
		o := r.runCRUDCreate(context.Background(), fx.Users[0], rng)
		if o.AppFailure || o.GenFailure {
			t.Fatalf("unexpected create failure: %+v", o)
		}
	}
	entries, err := state.ReadLedger(dir)
	if err != nil || len(entries) < 2 {
		t.Fatalf("expected durable ledger entries recorded, err=%v len=%d", err, len(entries))
	}
}
