package verifier

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/telemetry"
)

func seedCampaignDir(t *testing.T, elapsedHours float64, heapDriftMiB float64, leaks int64, includeFaults bool) string {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Duration(elapsedHours * float64(time.Hour)))

	cp := state.Checkpoint{
		CampaignID:          "W11-05-168H-UNIT",
		Mode:                "soak",
		Status:              state.StatusSoakCompletedPendingAudit,
		OfficialStartUTC:    start,
		EarliestValidEndUTC: start.Add(168 * time.Hour),
		TargetDurationSec:   (168 * time.Hour).Seconds(),
		LastHeartbeatUTC:    end,
		ProcessStarts:       1,
		AttemptedOps:        500000,
		SuccessfulOps:       499900,
		FailedAppOps:        100,
		CrossTenantChecks:   25000,
		CrossTenantLeaks:    leaks,
	}
	if err := state.WriteJSON(filepath.Join(dir, "state.json"), cp); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteJSON(filepath.Join(dir, "candidate-manifest.json"), state.CandidateManifest{
		CampaignID:   cp.CampaignID,
		GitCommitSHA: "d7b9a3dcd55ea14beadeeecaec065b23eeb5f345",
	}); err != nil {
		t.Fatal(err)
	}

	s, err := state.Open(dir, cp.CampaignID, "soak", 168*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	// Restore exact LastHeartbeatUTC for deterministic test
	_ = state.WriteJSON(filepath.Join(dir, "state.json"), cp)
	_ = os.Remove(filepath.Join(dir, "interruptions.jsonl"))

	totalSamples := max(int(elapsedHours*60), 20)
	for i := range totalSamples {
		frac := float64(i) / float64(max(totalSamples-1, 1))
		heapBytes := uint64((100.0 + heapDriftMiB*frac) * 1024 * 1024)
		ops := &telemetry.RuxbaseOpsSnapshot{}
		ops.Runtime.HeapAllocBytes = heapBytes
		ops.Runtime.Goroutines = 40
		_ = s.AppendJSONL("metrics.jsonl", telemetry.Sample{
			TimestampUTC:  start.Add(time.Duration(i) * time.Minute).Format(time.RFC3339),
			ElapsedSec:    float64(i * 60),
			HealthReadyOK: true,
			RuxbaseOps:    ops,
			Host:          telemetry.HostMetrics{DiskFreePct: 50.0},
		})
	}
	_ = s.AppendJSONL("backups.jsonl", map[string]any{"passed": true})
	if includeFaults {
		for _, drill := range []string{"hour24_worker_kill", "hour72_pg_disconnect", "hour120_backup_restore"} {
			_ = s.AppendJSONL("faults.jsonl", map[string]any{"drill": drill, "passed": true})
		}
	}
	return dir
}

func TestReconcileAndA11AuditVerdicts(t *testing.T) {
	// 1. Complete 168h clean campaign -> PASS and W11-05_VERIFIED
	dirOK := seedCampaignDir(t, 168.05, 1.5, 0, true)
	rep, err := Reconcile(dirOK, 168.0)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rep.Verdict != "PASS" {
		t.Fatalf("expected PASS, got %s (missing=%v failures=%v)", rep.Verdict, rep.MissingEvidence, rep.FailureReasons)
	}
	if _, _, err := state.WriteSHA256Manifest(dirOK); err != nil {
		t.Fatalf("WriteSHA256Manifest: %v", err)
	}
	av, err := AuditCampaign(dirOK, 168.0)
	if err != nil {
		t.Fatalf("AuditCampaign: %v", err)
	}
	if av.Verdict != "W11-05_VERIFIED" || !av.ManifestVerified {
		t.Fatalf("expected W11-05_VERIFIED, got %+v", av)
	}

	// 2. Shortened run (12h < 168h) -> INCOMPLETE / W11-05_INCOMPLETE_EVIDENCE
	dirShort := seedCampaignDir(t, 12.0, 0.5, 0, false)
	repShort, _ := Reconcile(dirShort, 168.0)
	if repShort.Verdict != "INCOMPLETE" {
		t.Fatalf("shortened 12h campaign must be INCOMPLETE, got %s", repShort.Verdict)
	}
	avShort, _ := AuditCampaign(dirShort, 168.0)
	if avShort.Verdict != "W11-05_INCOMPLETE_EVIDENCE" {
		t.Fatalf("expected W11-05_INCOMPLETE_EVIDENCE, got %s", avShort.Verdict)
	}

	// 3. Cross-tenant leak -> FAIL / W11-05_FAILED
	dirLeak := seedCampaignDir(t, 168.05, 0.5, 1, true)
	_, _, _ = state.WriteSHA256Manifest(dirLeak)
	avLeak, _ := AuditCampaign(dirLeak, 168.0)
	if avLeak.Verdict != "W11-05_FAILED" {
		t.Fatalf("cross-tenant leak must produce W11-05_FAILED, got %s", avLeak.Verdict)
	}

	// 4. Memory drift > 5% -> FAIL
	dirDrift := seedCampaignDir(t, 168.05, 12.0, 0, true)
	repDrift, _ := Reconcile(dirDrift, 168.0)
	if repDrift.Verdict != "FAIL" {
		t.Fatalf("expected FAIL for >5%% heap drift (%.2f%%), got %s", repDrift.Trends.HeapDriftPct, repDrift.Verdict)
	}

	// 5. Tampered evidence after manifest freeze -> W11-05_FAILED
	if err := os.WriteFile(filepath.Join(dirOK, "backups.jsonl"), []byte(`{"passed":true,"tampered":true}`+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	avTamper, _ := AuditCampaign(dirOK, 168.0)
	if avTamper.Verdict != "W11-05_FAILED" || avTamper.ManifestVerified {
		t.Fatalf("tampered artifact must fail A11 audit, got %+v", avTamper)
	}
}
