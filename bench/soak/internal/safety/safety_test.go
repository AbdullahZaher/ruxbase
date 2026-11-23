package safety

import (
	"strings"
	"testing"
)

func TestEvaluateFailClosedTriggers(t *testing.T) {
	th := DefaultThresholds()

	// Healthy observation continues
	if d := Evaluate(th, Observation{DiskFreePct: 55.0, RetainedWALBytes: 64 * 1024 * 1024, WindowTotalOps: 500, WindowErrorRatePct: 0.2}); d.Stop {
		t.Fatalf("healthy observation unexpectedly stopped: %+v", d)
	}

	// Cross-tenant leak MUST stop even during controlled fault window
	if d := Evaluate(th, Observation{CrossTenantLeaks: 1, ControlledFaultActive: true}); !d.Stop || d.Code != "cross_tenant_leak" {
		t.Fatalf("expected immediate stop on cross-tenant leak, got %+v", d)
	}

	// Storage checksum violation MUST stop
	if d := Evaluate(th, Observation{StorageChecksumErrors: 1}); !d.Stop || d.Code != "storage_checksum_mismatch" {
		t.Fatalf("expected stop on checksum mismatch, got %+v", d)
	}

	// Low disk headroom MUST stop
	if d := Evaluate(th, Observation{DiskFreePct: 12.5}); !d.Stop || d.Code != "disk_exhaustion_risk" {
		t.Fatalf("expected stop on low disk headroom, got %+v", d)
	}

	// Unbounded WAL retention MUST stop
	if d := Evaluate(th, Observation{RetainedWALBytes: 950 * 1024 * 1024}); !d.Stop || d.Code != "unbounded_wal_growth" {
		t.Fatalf("expected stop on WAL retention breach, got %+v", d)
	}
}

func TestCheckHostIsolationBlocksSharedProductionHost(t *testing.T) {
	// Dedicated staging host passes
	if err := CheckHostIsolation([]string{"ruxbase-soak-postgres", "ruxbase-soak-app", "ruxbase-soak-prometheus"}, "ruxbase-soak-"); err != nil {
		t.Fatalf("expected dedicated staging containers to pass, got %v", err)
	}

	// Shared production host MUST fail closed
	err := CheckHostIsolation([]string{"wrd-production-api", "vibress-prod-postgres-1", "ruxbase-soak-app"}, "ruxbase-soak-")
	if err == nil || !strings.Contains(err.Error(), "ISOLATION BLOCKER") {
		t.Fatalf("expected ISOLATION BLOCKER for shared production host, got %v", err)
	}
}
