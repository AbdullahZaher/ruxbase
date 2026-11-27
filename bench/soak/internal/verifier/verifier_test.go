package verifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/journeys"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/telemetry"
)

func seedCampaignDir(t *testing.T, elapsedHours float64, liveHeapDriftMiB float64, leaks int64, includeFaults bool) string {
	t.Helper()
	dir := t.TempDir()
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Duration(elapsedHours * float64(time.Hour)))
	cid := "W11-05-168H-UNIT"

	cp := state.Checkpoint{
		CampaignID:            cid,
		Mode:                  "soak",
		Status:                state.StatusSoakCompletedPendingAudit,
		OfficialStartUTC:      start,
		EarliestValidEndUTC:   start.Add(168 * time.Hour),
		TargetDurationSec:     (168 * time.Hour).Seconds(),
		LastHeartbeatUTC:      end,
		ProcessStarts:         1,
		AttemptedOps:          500000,
		SuccessfulOps:         499900,
		HTTPRequestsAttempted: 620000,
		HTTPRequestsSucceeded: 619900,
		MutationsAttempted:    120000,
		MutationsCommitted:    120000,
		FailedAppOps:          100,
		CrossTenantChecks:     25000,
		CrossTenantLeaks:      leaks,
	}
	if err := state.WriteJSON(filepath.Join(dir, "state.json"), cp); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteJSON(filepath.Join(dir, "candidate-manifest.json"), state.CandidateManifest{
		CampaignID:             cid,
		GitCommitSHA:           "d7b9a3dcd55ea14beadeeecaec065b23eeb5f345",
		LinuxBinarySHA256:      "abc123",
		DeployedBinarySHA256:   "abc123",
		DeployedBinaryVerified: true,
		PostgresVersion:        "18.7",
		AppliedMigrationsCount: 15,
		IsolationPassed:        true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteJSON(filepath.Join(dir, "worker-heartbeat.json"), state.WorkerHeartbeat{
		CampaignID:       cid,
		PID:              101,
		StartedAtUTC:     start.Format(time.RFC3339),
		LastHeartbeatUTC: end.Format(time.RFC3339),
		JobsCompleted:    1000,
	}); err != nil {
		t.Fatal(err)
	}

	s, err := state.Open(dir, cid, "soak", 168*time.Hour)
	// Expected error when reopening terminal state; write JSONL files cleanly via os.OpenFile/state.WriteJSON
	_ = err
	_ = s

	totalSamples := max(int(elapsedHours*60), 20)
	fMet, err := os.Create(filepath.Join(dir, "metrics.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	fWork, err := os.Create(filepath.Join(dir, "workload.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for i := range totalSamples {
		frac := float64(i) / float64(max(totalSamples-1, 1))
		liveBytes := uint64((100.0 + liveHeapDriftMiB*frac) * 1024 * 1024)
		// Deliberately inject 30% GC sawtooth oscillation in HeapAllocBytes while LiveHeapBytes stays steady
		sawtooth := uint64(((i % 3) * 12) * 1024 * 1024)
		ops := &telemetry.RuxbaseOpsSnapshot{}
		ops.Runtime.LiveHeapBytes = liveBytes
		ops.Runtime.HeapAllocBytes = liveBytes + sawtooth
		ops.Runtime.Goroutines = 40
		ts := start.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		_ = appendTestLine(fMet, telemetry.Sample{
			TimestampUTC:  ts,
			ElapsedSec:    float64(i * 60),
			HealthReadyOK: true,
			RuxbaseOps:    ops,
			Host:          telemetry.HostMetrics{DiskFreePct: 50.0},
		})
		_ = appendTestLine(fWork, map[string]any{
			"timestampUtc": ts,
			"success":      300,
		})
	}
	_ = fMet.Close()
	_ = fWork.Close()

	if err := writeTestLineFile(filepath.Join(dir, "integrity.jsonl"), IntegrityCheckReport{
		TimestampUTC:          end.Format(time.RFC3339),
		CampaignID:            cid,
		Passed:                true,
		RLSEnabled:            true,
		RuntimeRoleRestricted: true,
		ReleaseBlockersPassed: true,
		DirectSQLVisibility:   "OWNER_VISIBLE",
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeTestLineFile(filepath.Join(dir, "backups.jsonl"), map[string]any{"campaignId": cid, "passed": true}); err != nil {
		t.Fatal(err)
	}
	if includeFaults {
		fFault, err := os.Create(filepath.Join(dir, "faults.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, drill := range []string{"hour24_worker_kill", "hour72_pg_disconnect", "hour120_backup_restore"} {
			_ = appendTestLine(fFault, map[string]any{"drill": drill, "passed": true})
		}
		_ = fFault.Close()
	}
	return dir
}

func appendTestLine(f *os.File, v any) error {
	b, err := jsonMarshal(v)
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

func writeTestLineFile(path string, v any) error {
	b, err := jsonMarshal(v)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o640)
}

func TestReconstructExpectedLedgerLifecycle(t *testing.T) {
	okTrue := true
	okFalse := false
	entries := []state.LedgerEntry{
		{Seq: 1, OpID: "op-1", Phase: "intent", Kind: "record_create", TenantID: "t0", ExpectedText: "T1", ExpectedHash: journeys.CanonicalRecordChecksum("op-1", "T1")},
		{Seq: 2, OpID: "op-1", Phase: "ack", Kind: "record_create", TenantID: "t0", EntityID: "id1", ExpectedText: "T1", ExpectedHash: journeys.CanonicalRecordChecksum("op-1", "T1"), Committed: &okTrue},
		{Seq: 3, OpID: "op-2", Phase: "intent", Kind: "record_update", TenantID: "t0", EntityID: "id1", ExpectedText: "T2", ExpectedHash: journeys.CanonicalRecordChecksum("op-2", "T2")},
		{Seq: 4, OpID: "op-2", Phase: "ack", Kind: "record_update", TenantID: "t0", EntityID: "id1", ExpectedText: "T2", ExpectedHash: journeys.CanonicalRecordChecksum("op-2", "T2"), Committed: &okTrue},
		{Seq: 5, OpID: "op-3", Phase: "intent", Kind: "record_create", TenantID: "t0", ExpectedText: "T3", ExpectedHash: journeys.CanonicalRecordChecksum("op-3", "T3")},
		{Seq: 6, OpID: "op-3", Phase: "ack", Kind: "record_create", TenantID: "t0", EntityID: "id2", ExpectedText: "T3", ExpectedHash: journeys.CanonicalRecordChecksum("op-3", "T3"), Committed: &okTrue},
		{Seq: 7, OpID: "op-4", Phase: "intent", Kind: "record_delete", TenantID: "t0", EntityID: "id2", Deleted: true},
		{Seq: 8, OpID: "op-4", Phase: "ack", Kind: "record_delete", TenantID: "t0", EntityID: "id2", Deleted: true, Committed: &okTrue},
		// Rejected update should leave id1 at op-2 state!
		{Seq: 9, OpID: "op-5", Phase: "intent", Kind: "record_update", TenantID: "t0", EntityID: "id1", ExpectedText: "BAD"},
		{Seq: 10, OpID: "op-5", Phase: "ack", Kind: "record_update", TenantID: "t0", EntityID: "id1", ExpectedText: "BAD", Committed: &okFalse},
	}

	rec := ReconstructExpectedLedger(entries)
	if len(rec.Malformations) != 0 || len(rec.OrderedEntities) != 2 {
		t.Fatalf("unexpected reconstruction: %+v", rec)
	}
	if rec.OrderedEntities[0].OpID != "op-2" || rec.OrderedEntities[0].ExpectedText != "T2" || !rec.OrderedEntities[0].ExpectedPresent {
		t.Fatalf("id1 should reflect committed op-2 (not rejected op-5), got %+v", rec.OrderedEntities[0])
	}
	if !rec.OrderedEntities[1].Deleted || rec.OrderedEntities[1].ExpectedPresent {
		t.Fatalf("id2 should be marked Deleted, got %+v", rec.OrderedEntities[1])
	}
}

func TestReconcileAndA11AuditVerdictsAndCompletenessGates(t *testing.T) {
	// 1. Complete 168h clean campaign (even with 30% GC HeapAlloc sawtooth, steady LiveHeapBytes passes 5% gate!)
	dirOK := seedCampaignDir(t, 168.05, 1.5, 0, true)
	rep, err := Reconcile(dirOK, 168.0)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if rep.Verdict != "PASS" || rep.Trends.MemoryMetricUsed != "live_heap_post_gc" {
		t.Fatalf("expected PASS using live_heap_post_gc, got verdict=%s metric=%s (missing=%v failures=%v)", rep.Verdict, rep.Trends.MemoryMetricUsed, rep.MissingEvidence, rep.FailureReasons)
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

	// 4. Live-heap drift > 5% -> FAIL
	dirDrift := seedCampaignDir(t, 168.05, 12.0, 0, true)
	repDrift, _ := Reconcile(dirDrift, 168.0)
	if repDrift.Verdict != "FAIL" {
		t.Fatalf("expected FAIL for >5%% heap drift (%.2f%%), got %s", repDrift.Trends.HeapDriftPct, repDrift.Verdict)
	}

	// 5. Idle worker (zero active HTTP workload windows) -> FAIL
	dirIdle := seedCampaignDir(t, 168.05, 0.5, 0, true)
	_ = writeTestLineFile(filepath.Join(dirIdle, "workload.jsonl"), map[string]any{"timestampUtc": "2026-11-16T00:00:00Z", "success": 0})
	repIdle, _ := Reconcile(dirIdle, 168.0)
	if repIdle.Verdict != "FAIL" || !strings.Contains(strings.Join(repIdle.FailureReasons, ";"), "idle worker cannot satisfy load continuity") {
		t.Fatalf("expected idle workload failure, got %+v", repIdle)
	}

	// 6. Missing backup evidence -> INCOMPLETE
	dirNoBackup := seedCampaignDir(t, 168.05, 0.5, 0, true)
	_ = os.Remove(filepath.Join(dirNoBackup, "backups.jsonl"))
	repNoBkp, _ := Reconcile(dirNoBackup, 168.0)
	if repNoBkp.Verdict != "INCOMPLETE" {
		t.Fatalf("expected INCOMPLETE when backups.jsonl is missing, got %s", repNoBkp.Verdict)
	}

	// 7. Unverified integrity check -> FAIL
	dirBadInt := seedCampaignDir(t, 168.05, 0.5, 0, true)
	_ = writeTestLineFile(filepath.Join(dirBadInt, "integrity.jsonl"), IntegrityCheckReport{Passed: false, VerificationIncomplete: true, Failures: []string{"HIDDEN_BY_RLS"}})
	repBadInt, _ := Reconcile(dirBadInt, 168.0)
	if repBadInt.Verdict != "FAIL" {
		t.Fatalf("expected FAIL when integrity check is incomplete/failed, got %s", repBadInt.Verdict)
	}

	// 8. Tampered artifact after manifest freeze -> W11-05_FAILED
	if err := os.WriteFile(filepath.Join(dirOK, "backups.jsonl"), []byte(`{"campaignId":"W11-05-168H-UNIT","passed":true,"tampered":true}`+"\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	avTamper, _ := AuditCampaign(dirOK, 168.0)
	if avTamper.Verdict != "W11-05_FAILED" || avTamper.ManifestVerified {
		t.Fatalf("tampered artifact must fail A11 audit, got %+v", avTamper)
	}

	// 9. Corrupt metrics.jsonl -> FAIL (never silently skip malformed telemetry)
	dirCorruptMet := seedCampaignDir(t, 168.05, 0.5, 0, true)
	_ = os.WriteFile(filepath.Join(dirCorruptMet, "metrics.jsonl"), []byte("{not-json\n"), 0o640)
	repCorruptMet, _ := Reconcile(dirCorruptMet, 168.0)
	if repCorruptMet.Verdict != "FAIL" {
		t.Fatalf("expected FAIL for corrupt metrics.jsonl, got %s", repCorruptMet.Verdict)
	}

	// 10. Missing /api/ops/metrics runtime heap telemetry -> FAIL
	dirNoOps := seedCampaignDir(t, 168.05, 0.5, 0, true)
	_ = writeTestLineFile(filepath.Join(dirNoOps, "metrics.jsonl"), telemetry.Sample{
		TimestampUTC:  "2026-11-16T00:00:00Z",
		ElapsedSec:    60,
		HealthReadyOK: true,
		Host:          telemetry.HostMetrics{DiskFreePct: 50.0},
	})
	repNoOps, _ := Reconcile(dirNoOps, 168.0)
	if repNoOps.Verdict != "FAIL" {
		t.Fatalf("expected FAIL when runtime heap telemetry is missing, got %s (%v)", repNoOps.Verdict, repNoOps.FailureReasons)
	}
}
