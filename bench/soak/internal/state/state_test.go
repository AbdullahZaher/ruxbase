package state

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpenNeverResetsQualificationClockOnRestart(t *testing.T) {
	dir := t.TempDir()
	const cid = "W11-05-168H-TEST"

	s1, err := Open(dir, cid, "soak", 168*time.Hour)
	if err != nil {
		t.Fatalf("initial Open failed: %v", err)
	}
	cp1 := s1.Snapshot()
	if cp1.ProcessStarts != 1 {
		t.Fatalf("want processStarts=1, got %d", cp1.ProcessStarts)
	}
	if cp1.EarliestValidEndUTC.Sub(cp1.OfficialStartUTC) != 168*time.Hour {
		t.Fatalf("want exact 168h target window, got %v", cp1.EarliestValidEndUTC.Sub(cp1.OfficialStartUTC))
	}

	time.Sleep(20 * time.Millisecond)
	s2, err := Open(dir, cid, "soak", 168*time.Hour)
	if err != nil {
		t.Fatalf("resume Open failed: %v", err)
	}
	cp2 := s2.Snapshot()
	if !cp2.OfficialStartUTC.Equal(cp1.OfficialStartUTC) {
		t.Fatalf("qualification start clock was illegally reset: before=%v after=%v", cp1.OfficialStartUTC, cp2.OfficialStartUTC)
	}
	if !cp2.EarliestValidEndUTC.Equal(cp1.EarliestValidEndUTC) {
		t.Fatalf("earliest valid end clock was illegally modified: before=%v after=%v", cp1.EarliestValidEndUTC, cp2.OfficialStartUTC)
	}
	if cp2.ProcessStarts != 2 {
		t.Fatalf("want processStarts=2 after restart, got %d", cp2.ProcessStarts)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "interruptions.jsonl"))
	if err != nil || !strings.Contains(string(raw), "process_restart") {
		t.Fatalf("expected process_restart interruption record, err=%v body=%q", err, string(raw))
	}

	if _, err := Open(dir, "DIFFERENT-ID", "soak", 168*time.Hour); err == nil {
		t.Fatal("expected error when opening existing campaign dir with mismatched campaign ID")
	}
}

func TestOpenRefusesTerminalFailedCampaignReuse(t *testing.T) {
	dir := t.TempDir()
	const cid = "W11-05-PILOT-FAILED"
	s, err := Open(dir, cid, "pilot", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(cp *Checkpoint) {
		cp.Status = StatusPilotFailed
		cp.EmergencyStopped = true
		cp.EmergencyStopReason = "test_stop"
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(dir, cid, "pilot", time.Hour); err == nil || !strings.Contains(err.Error(), "refusing to reuse terminal") {
		t.Fatalf("expected terminal campaign reuse error, got %v", err)
	}
}

func TestLedgerMonotonicSeqAndPersistFailurePropagation(t *testing.T) {
	dir := t.TempDir()
	const cid = "W11-05-LEDGER-TEST"
	s, err := Open(dir, cid, "pilot", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	seq1, err := s.AppendLedger(LedgerEntry{OpID: "op-1", Phase: "intent", Kind: "record_create", TenantID: "t0"})
	if err != nil || seq1 != 1 {
		t.Fatalf("want seq=1 err=nil, got seq=%d err=%v", seq1, err)
	}
	seq2, err := s.AppendLedger(LedgerEntry{OpID: "op-1", Phase: "ack", Kind: "record_create", TenantID: "t0", EntityID: "e1"})
	if err != nil || seq2 != 2 {
		t.Fatalf("want seq=2 err=nil, got seq=%d err=%v", seq2, err)
	}

	// Simulate ledger persistence failure by replacing ledger.jsonl with a directory
	_ = os.Remove(filepath.Join(dir, "ledger.jsonl"))
	if err := os.Mkdir(filepath.Join(dir, "ledger.jsonl"), 0o750); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendLedger(LedgerEntry{OpID: "op-2", Phase: "intent", Kind: "record_create", TenantID: "t0"}); err == nil {
		t.Fatal("expected AppendLedger to fail when ledger.jsonl cannot be opened")
	}
	if got := s.Snapshot().LedgerPersistErrors; got != 1 {
		t.Fatalf("expected LedgerPersistErrors=1, got %d", got)
	}
}

func TestMetricsHandlerAndSHA256ManifestVerification(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "W11-05-168H-PROM", "soak", 168*time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = s.Update(func(cp *Checkpoint) {
		cp.AttemptedOps = 100
		cp.SuccessfulOps = 99
		cp.HTTPRequestsAttempted = 140
		cp.HTTPRequestsSucceeded = 139
		cp.FailedAppOps = 1
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.MetricsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 on /metrics, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "ruxbase_soak_ops_success_total 99") || !strings.Contains(body, "ruxbase_soak_http_requests_success_total 139") {
		t.Fatalf("missing expected metrics in body: %s", body)
	}

	mPath, count, err := WriteSHA256Manifest(dir)
	if err != nil {
		t.Fatalf("WriteSHA256Manifest: %v", err)
	}
	if count < 1 {
		t.Fatalf("expected >=1 hashed file, got %d", count)
	}
	if _, err := os.Stat(mPath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	ok, mismatches, checked, err := VerifySHA256Manifest(dir)
	if err != nil || !ok || len(mismatches) > 0 || checked != count {
		t.Fatalf("VerifySHA256Manifest failed: ok=%v mismatches=%v checked=%d err=%v", ok, mismatches, checked, err)
	}
}
