package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/verifier"
)

func TestCmdReconcileManifestMatchesAllWrittenFiles(t *testing.T) {
	dir := t.TempDir()
	start := time.Date(2026, 11, 16, 0, 0, 0, 0, time.UTC)
	end := start.Add(168*time.Hour + 2*time.Minute)
	const cid = "W11-05-168H-CMD-TEST"

	if err := state.WriteJSON(filepath.Join(dir, "state.json"), state.Checkpoint{
		CampaignID:            cid,
		Mode:                  "soak",
		Status:                state.StatusSoakCompletedPendingAudit,
		OfficialStartUTC:      start,
		EarliestValidEndUTC:   start.Add(168 * time.Hour),
		TargetDurationSec:     (168 * time.Hour).Seconds(),
		LastHeartbeatUTC:      end,
		AttemptedOps:          1000,
		SuccessfulOps:         1000,
		HTTPRequestsAttempted: 1200,
		HTTPRequestsSucceeded: 1200,
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteJSON(filepath.Join(dir, "candidate-manifest.json"), state.CandidateManifest{
		CampaignID:             cid,
		GitCommitSHA:           "d7b9a3dc",
		LinuxBinarySHA256:      "deadbeef",
		DeployedBinarySHA256:   "deadbeef",
		DeployedBinaryVerified: true,
		PostgresVersion:        "18.7",
		AppliedMigrationsCount: 15,
		IsolationPassed:        true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.WriteJSON(filepath.Join(dir, "worker-heartbeat.json"), state.WorkerHeartbeat{
		CampaignID:       cid,
		PID:              1,
		StartedAtUTC:     start.Format(time.RFC3339),
		LastHeartbeatUTC: end.Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}

	fMet, _ := os.Create(filepath.Join(dir, "metrics.jsonl"))
	fWork, _ := os.Create(filepath.Join(dir, "workload.jsonl"))
	for i := range int(168*60) + 1 {
		ts := start.Add(time.Duration(i) * time.Minute).Format(time.RFC3339)
		_, _ = fMet.WriteString(`{"timestampUtc":"` + ts + `","elapsedSec":60,"healthReadyOk":true,"ruxbaseOps":{"runtime":{"goroutines":20,"heapAllocBytes":8388608,"liveHeapBytes":8388608}},"host":{"diskFreePct":50}}` + "\n")
		_, _ = fWork.WriteString(`{"timestampUtc":"` + ts + `","success":100}` + "\n")
	}
	_ = fMet.Close()
	_ = fWork.Close()

	_ = os.WriteFile(filepath.Join(dir, "integrity.jsonl"), []byte(`{"timestampUtc":"`+end.Format(time.RFC3339)+`","campaignId":"`+cid+`","passed":true}`+"\n"), 0o640)
	_ = os.WriteFile(filepath.Join(dir, "backups.jsonl"), []byte(`{"campaignId":"`+cid+`","passed":true}`+"\n"), 0o640)
	_ = os.WriteFile(filepath.Join(dir, "faults.jsonl"), []byte("{\"drill\":\"hour24_worker_kill\",\"passed\":true}\n{\"drill\":\"hour72_pg_disconnect\",\"passed\":true}\n{\"drill\":\"hour120_backup_restore\",\"passed\":true}\n"), 0o640)

	if err := cmdReconcile([]string{"--evidence-dir", dir, "--required-hours", "168"}); err != nil {
		t.Fatalf("cmdReconcile failed: %v", err)
	}
	if err := cmdAudit([]string{"--evidence-dir", dir, "--required-hours", "168"}); err != nil {
		t.Fatalf("cmdAudit failed after cmdReconcile: %v", err)
	}

	// Verify VerifySHA256Manifest passes with 0 mismatches (unlike v1.0.0 self-invalidation bug)
	ok, mismatches, checked, err := state.VerifySHA256Manifest(dir)
	if err != nil || !ok || len(mismatches) > 0 || checked < 6 {
		t.Fatalf("manifest verification failed: ok=%v mismatches=%v checked=%d err=%v", ok, mismatches, checked, err)
	}
	// Verify verifier.AuditCampaign independent check too
	av, err := verifier.AuditCampaign(dir, 168.0)
	if err != nil || av.Verdict != "W11-05_VERIFIED" {
		t.Fatalf("expected W11-05_VERIFIED, got %+v err=%v", av, err)
	}
}
