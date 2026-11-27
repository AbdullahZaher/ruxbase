// Package state manages durable campaign provenance, monotonic/wall-clock tracking,
// expected-results intent/ack ledgers, alert history, interruption records, and Prometheus exposition.
//
// CRITICAL INVARIANT: Restarting the soak process MUST NEVER silently reset the
// official 168-hour qualification clock, nor reuse a failed/completed campaign directory.
package state

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// CampaignStatus enumerates the explicit W11-05 governance lifecycle states.
type CampaignStatus string

const (
	StatusInfrastructurePreparing   CampaignStatus = "INFRASTRUCTURE_PREPARING"
	StatusPilotRunning              CampaignStatus = "PILOT_RUNNING"
	StatusPilotFailed               CampaignStatus = "PILOT_FAILED"
	StatusPilotPassed               CampaignStatus = "PILOT_PASSED"
	StatusSoakRunning               CampaignStatus = "SOAK_RUNNING"
	StatusSoakInterrupted           CampaignStatus = "SOAK_INTERRUPTED"
	StatusSoakCompletedPendingAudit CampaignStatus = "SOAK_COMPLETED_PENDING_AUDIT"
	StatusVerified                  CampaignStatus = "W11-05_VERIFIED"
	StatusFailed                    CampaignStatus = "W11-05_FAILED"
)

// CandidateManifest freezes all build, image, schema, and configuration provenance
// prior to launching the official 168-hour campaign.
type CandidateManifest struct {
	CampaignID              string            `json:"campaignId"`
	CreatedAtUTC            string            `json:"createdAtUtc"`
	GitCommitSHA            string            `json:"gitCommitSha"`
	GitTreeSHA              string            `json:"gitTreeSha"`
	LinuxBinarySHA256       string            `json:"linuxBinarySha256"`
	DeployedBinarySHA256    string            `json:"deployedBinarySha256,omitempty"`
	DeployedBinaryVerified  bool              `json:"deployedBinaryVerified"`
	GoVersion               string            `json:"goVersion"`
	ContainerImageDigests   map[string]string `json:"containerImageDigests"`
	PostgresVersion         string            `json:"postgresVersion"`
	ExtensionVersions       map[string]string `json:"extensionVersions"`
	AppliedMigrationsCount  int               `json:"appliedMigrationsCount"`
	AppliedMigrationsSHA256 string            `json:"appliedMigrationsSha256"`
	ConfigHashes            map[string]string `json:"configHashes"`
	IntegrityVerifierVer    string            `json:"integrityVerifierVersion"`
	TrafficScheduleCapacity float64           `json:"trafficScheduleCapacityRps"`
	IsolationPassed         bool              `json:"isolationPassed"`
	IsolationNote           string            `json:"isolationNote,omitempty"`
}

// Checkpoint persists durable campaign clock and cumulative operation counters.
type Checkpoint struct {
	CampaignID            string         `json:"campaignId"`
	Mode                  string         `json:"mode"` // "pilot" or "soak"
	Status                CampaignStatus `json:"status"`
	OfficialStartUTC      time.Time      `json:"officialStartUtc"`
	EarliestValidEndUTC   time.Time      `json:"earliestValidEndUtc"`
	TargetDurationSec     float64        `json:"targetDurationSec"`
	LastHeartbeatUTC      time.Time      `json:"lastHeartbeatUtc"`
	LastBootID            string         `json:"lastBootId,omitempty"`
	ProcessStarts         int            `json:"processStarts"`
	AttemptedOps          int64          `json:"attemptedOps"`
	SuccessfulOps         int64          `json:"successfulOps"`
	HTTPRequestsAttempted int64          `json:"httpRequestsAttempted"`
	HTTPRequestsSucceeded int64          `json:"httpRequestsSucceeded"`
	MutationsAttempted    int64          `json:"mutationsAttempted"`
	MutationsCommitted    int64          `json:"mutationsCommitted"`
	JourneysCompleted     int64          `json:"journeysCompleted"`
	NoOpSkipped           int64          `json:"noOpSkipped"`
	LedgerPersistErrors   int64          `json:"ledgerPersistErrors"`
	FailedAppOps          int64          `json:"failedAppOps"`
	FailedGenOps          int64          `json:"failedGenOps"`
	TimedOutOps           int64          `json:"timedOutOps"`
	CrossTenantChecks     int64          `json:"crossTenantChecks"`
	CrossTenantLeaks      int64          `json:"crossTenantLeaks"`
	IntegrityViolations   int64          `json:"integrityViolations"`
	CriticalAlerts        int64          `json:"criticalAlerts"`
	EmergencyStopped      bool           `json:"emergencyStopped"`
	EmergencyStopReason   string         `json:"emergencyStopReason,omitempty"`
	CurrentTargetRPS      float64        `json:"currentTargetRps"`
	CurrentAchievedRPS    float64        `json:"currentAchievedRps"`
	CurrentWindow         string         `json:"currentWindow,omitempty"`
	LastP50Ms             float64        `json:"lastP50Ms"`
	LastP95Ms             float64        `json:"lastP95Ms"`
	LastP99Ms             float64        `json:"lastP99Ms"`
	ActiveSSEClients      int            `json:"activeSseClients"`
	LastBackupStatus      string         `json:"lastBackupStatus,omitempty"`
	LastBackupUTC         string         `json:"lastBackupUtc,omitempty"`
}

// LedgerEntry records a synthetic mutation intent or acknowledgment for PostgreSQL reconciliation.
type LedgerEntry struct {
	Seq          int64  `json:"seq,omitempty"`
	OpID         string `json:"opId"`
	Phase        string `json:"phase,omitempty"` // "intent" or "ack" (empty = legacy single entry)
	TimestampUTC string `json:"timestampUtc"`
	Kind         string `json:"kind"` // record_create, record_update, record_delete, storage_object
	TenantID     string `json:"tenantId"`
	Collection   string `json:"collection,omitempty"`
	EntityID     string `json:"entityId,omitempty"`
	ExpectedHash string `json:"expectedHash,omitempty"`
	ExpectedText string `json:"expectedText,omitempty"`
	HTTPStatus   int    `json:"httpStatus,omitempty"`
	Committed    *bool  `json:"committed,omitempty"`
	Error        string `json:"error,omitempty"`
	Deleted      bool   `json:"deleted,omitempty"`
}

// WorkerHeartbeat is persisted periodically by soak-worker inside the active campaign directory.
type WorkerHeartbeat struct {
	CampaignID         string `json:"campaignId"`
	PID                int    `json:"pid"`
	StartedAtUTC       string `json:"startedAtUtc"`
	LastHeartbeatUTC   string `json:"lastHeartbeatUtc"`
	JobsEnqueued       int64  `json:"jobsEnqueued"`
	JobsAttempted      int64  `json:"jobsAttempted"`
	JobsCompleted      int64  `json:"jobsCompleted"`
	JobsRetried        int64  `json:"jobsRetried"`
	JobsReclaimed      int64  `json:"jobsReclaimed"`
	WorkflowsCompleted int64  `json:"workflowsCompleted"`
	OutboxPublished    int64  `json:"outboxPublished"`
	MultipartCompleted int64  `json:"multipartCompleted"`
	MultipartErrors    int64  `json:"multipartErrors"`
	OrphanGCCleaned    int64  `json:"orphanGcCleaned"`
	IntegrityErrors    int64  `json:"integrityErrors"`
}

// InterruptionEvent records any process restart, workload gap, telemetry gap, or VPS reboot.
type InterruptionEvent struct {
	TimestampUTC   string  `json:"timestampUtc"`
	Kind           string  `json:"kind"` // process_restart, workload_gap, telemetry_gap, host_reboot, controlled_fault, emergency_stop
	GapSeconds     float64 `json:"gapSeconds"`
	Detail         string  `json:"detail"`
	Controlled     bool    `json:"controlled"`
	Disqualifying  bool    `json:"disqualifying"`
	RecoveredAtUTC string  `json:"recoveredAtUtc,omitempty"`
}

// AlertEvent records an operational alert with evidence and recovery context.
type AlertEvent struct {
	TimestampUTC string `json:"timestampUtc"`
	Severity     string `json:"severity"` // warning, critical
	Code         string `json:"code"`
	Component    string `json:"component"`
	Message      string `json:"message"`
	Evidence     string `json:"evidence,omitempty"`
	Recovery     string `json:"recovery,omitempty"`
}

// Store provides thread-safe durable persistence for a single campaign directory.
type Store struct {
	mu           sync.Mutex
	dir          string
	checkpoint   Checkpoint
	bootTimeMono time.Time
	ledgerSeq    int64
}

// IsTerminalStatus reports whether a campaign state may never be resumed or reused.
func IsTerminalStatus(st CampaignStatus) bool {
	switch st {
	case StatusPilotFailed, StatusPilotPassed, StatusSoakCompletedPendingAudit, StatusVerified, StatusFailed:
		return true
	default:
		return false
	}
}

// Open initializes or resumes an active campaign store under dir.
// If state.json already exists:
//   - refuses if CampaignID differs or if the campaign is in a terminal state (failed/completed)
//   - preserves OfficialStartUTC and EarliestValidEndUTC immutably
//   - appends a process_restart or host_reboot interruption event
func Open(dir, campaignID, mode string, targetDuration time.Duration) (*Store, error) {
	if campaignID == "" {
		return nil, fmt.Errorf("campaignID is required")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create campaign evidence dir %s: %w", dir, err)
	}

	s := &Store{
		dir:          dir,
		bootTimeMono: time.Now(),
	}

	statePath := filepath.Join(dir, "state.json")
	raw, err := os.ReadFile(statePath)
	now := time.Now().UTC()
	bootID := readHostBootID()

	if err == nil {
		var existing Checkpoint
		if uErr := json.Unmarshal(raw, &existing); uErr != nil {
			return nil, fmt.Errorf("corrupt checkpoint %s: %w", statePath, uErr)
		}
		if existing.CampaignID != campaignID {
			return nil, fmt.Errorf("campaign ID mismatch in %s: existing=%q requested=%q (refusing to overwrite)", statePath, existing.CampaignID, campaignID)
		}
		if IsTerminalStatus(existing.Status) || existing.EmergencyStopped {
			return nil, fmt.Errorf("refusing to reuse terminal/stopped campaign %q (status=%s emergencyStopped=%v); generate a new campaign ID", existing.CampaignID, existing.Status, existing.EmergencyStopped)
		}
		if existing.Mode != "" && mode != "" && existing.Mode != mode {
			return nil, fmt.Errorf("campaign mode mismatch for %q: existing=%q requested=%q", campaignID, existing.Mode, mode)
		}

		// Preserve monotonic ledger sequence across process restarts
		if prevEntries, rErr := ReadLedger(dir); rErr == nil {
			for _, pe := range prevEntries {
				if pe.Seq > s.ledgerSeq {
					s.ledgerSeq = pe.Seq
				}
			}
			if int64(len(prevEntries)) > s.ledgerSeq {
				s.ledgerSeq = int64(len(prevEntries))
			}
		}

		gapSec := 0.0
		if !existing.LastHeartbeatUTC.IsZero() {
			gapSec = now.Sub(existing.LastHeartbeatUTC).Seconds()
			if gapSec < 0 {
				gapSec = 0
			}
		}
		existing.ProcessStarts++
		rebooted := existing.LastBootID != "" && bootID != "" && existing.LastBootID != bootID
		existing.LastBootID = bootID
		existing.LastHeartbeatUTC = now
		s.checkpoint = existing

		kind := "process_restart"
		detail := fmt.Sprintf("soak process restarted (start #%d); original start time %s preserved", existing.ProcessStarts, existing.OfficialStartUTC.Format(time.RFC3339))
		if rebooted {
			kind = "host_reboot"
			detail = fmt.Sprintf("VPS reboot detected (boot_id changed); original start time %s preserved", existing.OfficialStartUTC.Format(time.RFC3339))
		}
		if aErr := s.AppendInterruption(InterruptionEvent{
			TimestampUTC:   now.Format(time.RFC3339),
			Kind:           kind,
			GapSeconds:     gapSec,
			Detail:         detail,
			Controlled:     false,
			Disqualifying:  gapSec > 300,
			RecoveredAtUTC: now.Format(time.RFC3339),
		}); aErr != nil {
			return nil, fmt.Errorf("record restart interruption: %w", aErr)
		}
		if err := s.saveLocked(); err != nil {
			return nil, err
		}
		return s, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read checkpoint %s: %w", statePath, err)
	}

	status := StatusSoakRunning
	if mode == "pilot" {
		status = StatusPilotRunning
	}
	s.checkpoint = Checkpoint{
		CampaignID:          campaignID,
		Mode:                mode,
		Status:              status,
		OfficialStartUTC:    now,
		EarliestValidEndUTC: now.Add(targetDuration),
		TargetDurationSec:   targetDuration.Seconds(),
		LastHeartbeatUTC:    now,
		LastBootID:          bootID,
		ProcessStarts:       1,
	}
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return s, nil
}

// Dir returns the evidence directory path.
func (s *Store) Dir() string {
	return s.dir
}

// Snapshot returns a copy of the current checkpoint.
func (s *Store) Snapshot() Checkpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpoint
}

// Update mutates the checkpoint atomically and flushes it to disk via temp-file rename.
func (s *Store) Update(fn func(cp *Checkpoint)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.checkpoint)
	s.checkpoint.LastHeartbeatUTC = time.Now().UTC()
	return s.saveLocked()
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(&s.checkpoint, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, "state.json.tmp")
	dst := filepath.Join(s.dir, "state.json")
	if err := os.WriteFile(tmp, raw, 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// AppendJSONL appends a single JSON record to filename inside the evidence directory.
func (s *Store) AppendJSONL(filename string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.appendBytesLocked(filename, raw, false)
}

func (s *Store) appendBytesLocked(filename string, raw []byte, syncFile bool) error {
	f, err := os.OpenFile(filepath.Join(s.dir, filename), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(raw, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if syncFile {
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

// AppendLedger assigns a monotonic sequence number, durably appends+fsyncs an intent or
// acknowledgment ledger entry, and increments LedgerPersistErrors on failure.
func (s *Store) AppendLedger(entry LedgerEntry) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ledgerSeq++
	entry.Seq = s.ledgerSeq
	if entry.TimestampUTC == "" {
		entry.TimestampUTC = time.Now().UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		s.checkpoint.LedgerPersistErrors++
		return 0, err
	}
	if err := s.appendBytesLocked("ledger.jsonl", raw, true); err != nil {
		s.checkpoint.LedgerPersistErrors++
		_ = s.saveLocked()
		return 0, fmt.Errorf("persist ledger entry %s (%s): %w", entry.OpID, entry.Phase, err)
	}
	return entry.Seq, nil
}

// AppendInterruption records an outage/restart/fault interval.
func (s *Store) AppendInterruption(ev InterruptionEvent) error {
	return s.AppendJSONL("interruptions.jsonl", ev)
}

// AppendAlert records a warning or critical operational alert.
func (s *Store) AppendAlert(ev AlertEvent) error {
	if ev.Severity == "critical" {
		s.mu.Lock()
		s.checkpoint.CriticalAlerts++
		s.mu.Unlock()
	}
	return s.AppendJSONL("alerts.jsonl", ev)
}

// WriteJSON writes a standalone formatted JSON artifact atomically.
func WriteJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ReadLedger loads all expected-results ledger entries from a campaign directory, failing closed
// on any malformed JSONL line.
func ReadLedger(dir string) ([]LedgerEntry, error) {
	f, err := os.Open(filepath.Join(dir, "ledger.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var entries []LedgerEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e LedgerEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return nil, fmt.Errorf("parse ledger line %d: %w", lineNo, err)
		}
		entries = append(entries, e)
	}
	return entries, sc.Err()
}

// WriteSHA256Manifest computes SHA-256 checksums for every top-level artifact file in dir
// (excluding SHA256SUMS.txt and temporary files) and writes SHA256SUMS.txt atomically.
// Callers MUST NOT modify any hashed file after calling WriteSHA256Manifest.
func WriteSHA256Manifest(dir string) (string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", 0, err
	}
	names := make([]string, 0, len(entries))
	for _, de := range entries {
		if de.IsDir() || de.Name() == "SHA256SUMS.txt" || strings.HasSuffix(de.Name(), ".tmp") {
			continue
		}
		names = append(names, de.Name())
	}
	slices.Sort(names)

	var b strings.Builder
	for _, name := range names {
		sum, err := FileSHA256(filepath.Join(dir, name))
		if err != nil {
			return "", 0, err
		}
		fmt.Fprintf(&b, "%s  %s\n", sum, name)
	}

	manifestPath := filepath.Join(dir, "SHA256SUMS.txt")
	tmpPath := manifestPath + ".tmp"
	if err := os.WriteFile(tmpPath, []byte(b.String()), 0o640); err != nil {
		return "", 0, err
	}
	if err := os.Rename(tmpPath, manifestPath); err != nil {
		return "", 0, err
	}
	return manifestPath, len(names), nil
}

// VerifySHA256Manifest validates every entry in SHA256SUMS.txt against actual files in dir.
func VerifySHA256Manifest(dir string) (verified bool, mismatches []string, checked int, err error) {
	mRaw, err := os.ReadFile(filepath.Join(dir, "SHA256SUMS.txt"))
	if err != nil {
		return false, nil, 0, err
	}
	for _, line := range strings.Split(string(mRaw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			mismatches = append(mismatches, fmt.Sprintf("malformed manifest line: %q", line))
			continue
		}
		wantSum, name := parts[0], parts[1]
		checked++
		gotSum, hErr := FileSHA256(filepath.Join(dir, name))
		if hErr != nil || gotSum != wantSum {
			mismatches = append(mismatches, fmt.Sprintf("%s: want=%s got=%s err=%v", name, wantSum, gotSum, hErr))
		}
	}
	if checked == 0 {
		mismatches = append(mismatches, "SHA256SUMS.txt contains zero entries")
	}
	return len(mismatches) == 0, mismatches, checked, nil
}

// FileSHA256 returns the lowercase hex SHA-256 digest of path.
func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// MetricsHandler exposes Prometheus metrics and /healthz for the soak engine.
func (s *Store) MetricsHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		cp := s.Snapshot()
		if cp.EmergencyStopped {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("emergency_stopped\n"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		cp := s.Snapshot()
		elapsedSec := time.Since(cp.OfficialStartUTC).Seconds()
		if elapsedSec < 0 {
			elapsedSec = 0
		}
		pct := 0.0
		if cp.TargetDurationSec > 0 {
			pct = (elapsedSec / cp.TargetDurationSec) * 100.0
			if pct > 100.0 {
				pct = 100.0
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var b strings.Builder
		writeMetric := func(name, mtype, help string, val any) {
			fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %v\n", name, help, name, mtype, name, val)
		}
		writeMetric("ruxbase_soak_elapsed_seconds", "gauge", "Wall-clock seconds elapsed since official campaign start.", fmt.Sprintf("%.2f", elapsedSec))
		writeMetric("ruxbase_soak_completion_percent", "gauge", "Campaign completion percentage toward target duration.", fmt.Sprintf("%.4f", pct))
		writeMetric("ruxbase_soak_heartbeat_unix", "gauge", "Unix timestamp of last soak workload heartbeat.", cp.LastHeartbeatUTC.Unix())
		writeMetric("ruxbase_soak_process_starts_total", "counter", "Total soak process starts for this campaign.", cp.ProcessStarts)
		writeMetric("ruxbase_soak_ops_attempted_total", "counter", "Total attempted soak operations.", cp.AttemptedOps)
		writeMetric("ruxbase_soak_ops_success_total", "counter", "Total successful soak operations.", cp.SuccessfulOps)
		writeMetric("ruxbase_soak_http_requests_attempted_total", "counter", "Total HTTP requests attempted.", cp.HTTPRequestsAttempted)
		writeMetric("ruxbase_soak_http_requests_success_total", "counter", "Total HTTP requests succeeded.", cp.HTTPRequestsSucceeded)
		writeMetric("ruxbase_soak_mutations_attempted_total", "counter", "Total record/file mutations attempted.", cp.MutationsAttempted)
		writeMetric("ruxbase_soak_mutations_committed_total", "counter", "Total record/file mutations committed.", cp.MutationsCommitted)
		writeMetric("ruxbase_soak_noop_skipped_total", "counter", "Total skipped no-op actions.", cp.NoOpSkipped)
		writeMetric("ruxbase_soak_ledger_persist_errors_total", "counter", "Total durable ledger persistence errors.", cp.LedgerPersistErrors)
		writeMetric("ruxbase_soak_ops_failed_app_total", "counter", "Total failed operations attributed to application/DB.", cp.FailedAppOps)
		writeMetric("ruxbase_soak_ops_failed_gen_total", "counter", "Total failed operations attributed to load generator.", cp.FailedGenOps)
		writeMetric("ruxbase_soak_ops_timeout_total", "counter", "Total timed-out soak operations.", cp.TimedOutOps)
		writeMetric("ruxbase_soak_target_rps", "gauge", "Current scheduled target arrival RPS.", fmt.Sprintf("%.2f", cp.CurrentTargetRPS))
		writeMetric("ruxbase_soak_achieved_rps", "gauge", "Current window achieved RPS.", fmt.Sprintf("%.2f", cp.CurrentAchievedRPS))
		writeMetric("ruxbase_soak_latency_p50_ms", "gauge", "Recent window p50 latency in milliseconds.", fmt.Sprintf("%.3f", cp.LastP50Ms))
		writeMetric("ruxbase_soak_latency_p95_ms", "gauge", "Recent window p95 latency in milliseconds.", fmt.Sprintf("%.3f", cp.LastP95Ms))
		writeMetric("ruxbase_soak_latency_p99_ms", "gauge", "Recent window p99 latency in milliseconds.", fmt.Sprintf("%.3f", cp.LastP99Ms))
		writeMetric("ruxbase_soak_sse_clients_active", "gauge", "Active persistent SSE subscriber connections.", cp.ActiveSSEClients)
		writeMetric("ruxbase_soak_cross_tenant_checks_total", "counter", "Total negative cross-tenant RLS checks executed.", cp.CrossTenantChecks)
		writeMetric("ruxbase_soak_cross_tenant_leaks_total", "counter", "Total unauthorized cross-tenant data exposures detected (must be 0).", cp.CrossTenantLeaks)
		writeMetric("ruxbase_soak_integrity_violations_total", "counter", "Total data/storage/job integrity violations detected (must be 0).", cp.IntegrityViolations)
		writeMetric("ruxbase_soak_critical_alerts_total", "counter", "Total critical alerts emitted.", cp.CriticalAlerts)
		emerg := 0
		if cp.EmergencyStopped {
			emerg = 1
		}
		writeMetric("ruxbase_soak_emergency_stopped", "gauge", "1 if emergency stop was triggered, else 0.", emerg)
		backupOK := 0
		if cp.LastBackupStatus == "passed" {
			backupOK = 1
		}
		writeMetric("ruxbase_soak_last_backup_verified", "gauge", "1 if most recent online backup & isolated restore verified cleanly.", backupOK)
		_, _ = w.Write([]byte(b.String()))
	})
	return mux
}

func readHostBootID() string {
	raw, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(raw))
}
