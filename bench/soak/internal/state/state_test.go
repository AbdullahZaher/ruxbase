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

	// Simulate process restart
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
		t.Fatalf("earliest valid end clock was illegally modified: before=%v after=%v", cp1.EarliestValidEndUTC, cp2.EarliestValidEndUTC)
	}
	if cp2.ProcessStarts != 2 {
		t.Fatalf("want processStarts=2 after restart, got %d", cp2.ProcessStarts)
	}

	// Verify interruption event was durably logged
	raw, err := os.ReadFile(filepath.Join(dir, "interruptions.jsonl"))
	if err != nil || !strings.Contains(string(raw), "process_restart") {
		t.Fatalf("expected process_restart interruption record, err=%v body=%q", err, string(raw))
	}

	// Verify mismatched campaign ID is rejected (fail closed)
	if _, err := Open(dir, "DIFFERENT-ID", "soak", 168*time.Hour); err == nil {
		t.Fatal("expected error when opening existing campaign dir with mismatched campaign ID")
	}
}

func TestMetricsHandlerAndSHA256Manifest(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir, "W11-05-168H-PROM", "soak", 168*time.Hour)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = s.Update(func(cp *Checkpoint) {
		cp.AttemptedOps = 100
		cp.SuccessfulOps = 99
		cp.FailedAppOps = 1
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.MetricsHandler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 on /metrics, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ruxbase_soak_ops_success_total 99") {
		t.Fatalf("missing expected metric in body: %s", rec.Body.String())
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
}
