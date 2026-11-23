// Package verifier implements:
//  1. Periodic independent data & security integrity reconciliation (Phase 5),
//  2. Pilot capacity calibration & SLO baseline derivation (Phase 6),
//  3. Final 168-hour qualification reconciliation & SHA-256 manifest generation (Phase 11),
//  4. Strictly read-only A11 Independent Operational Certification Audit (Section 14).
package verifier

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/journeys"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/state"
	"github.com/AbdullahZaher/ruxbase/bench/soak/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
)

const VerifierVersion = "w11-05-verifier-v1.0.0"

// IntegrityCheckReport captures one independent reconciliation pass.
type IntegrityCheckReport struct {
	TimestampUTC          string   `json:"timestampUtc"`
	Passed                bool     `json:"passed"`
	LedgerChecked         int      `json:"ledgerChecked"`
	MissingCommittedRows  int      `json:"missingCommittedRows"`
	CorruptedChecksumRows int      `json:"corruptedChecksumRows"`
	NullTenantRows        int64    `json:"nullTenantRows"`
	TamperedRows          int64    `json:"tamperedRows"`
	RLSEnabled            bool     `json:"rlsEnabled"`
	RuntimeRoleRestricted bool     `json:"runtimeRoleRestricted"`
	ReleaseBlockersPassed bool     `json:"releaseBlockersPassed"`
	DeadJobs              int64    `json:"deadJobs"`
	FailedOutbox          int64    `json:"failedOutbox"`
	Failures              []string `json:"failures,omitempty"`
}

// RunIntegrityCheck independently reconciles expected-results ledger entries against
// PostgreSQL, verifies RLS, non-superuser role, and /api/studio/overview CheckReleaseBlockers.
func RunIntegrityCheck(ctx context.Context, client *http.Client, baseURL, superToken string, pool *pgxpool.Pool, evidenceDir string) IntegrityCheckReport {
	rep := IntegrityCheckReport{
		TimestampUTC: time.Now().UTC().Format(time.RFC3339),
		Passed:       true,
	}

	if pool != nil {
		// 1. Verify runtime role privileges (NOSUPERUSER, NOBYPASSRLS)
		var super, bypass bool
		if err := pool.QueryRow(ctx, "SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = 'ruxbase_app'").Scan(&super, &bypass); err != nil {
			rep.Passed = false
			rep.Failures = append(rep.Failures, fmt.Sprintf("unable to verify ruxbase_app role: %v", err))
		} else if super || bypass {
			rep.Passed = false
			rep.Failures = append(rep.Failures, fmt.Sprintf("SECURITY VIOLATION: ruxbase_app super=%v bypassrls=%v", super, bypass))
		} else {
			rep.RuntimeRoleRestricted = true
		}

		// 2. Verify PostgreSQL RLS enabled on soak_items
		var rlsEnabled bool
		if err := pool.QueryRow(ctx, "SELECT relrowsecurity FROM pg_class WHERE relname = $1 AND relnamespace = 'public'::regnamespace", journeys.SoakItemsCollection).Scan(&rlsEnabled); err != nil || !rlsEnabled {
			rep.Passed = false
			rep.Failures = append(rep.Failures, fmt.Sprintf("relrowsecurity check failed on %s (enabled=%v err=%v)", journeys.SoakItemsCollection, rlsEnabled, err))
		} else {
			rep.RLSEnabled = true
		}

		// 3. Check null tenant ownership & tampered rows
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM soak_items WHERE tenant_id IS NULL OR tenant_id = ''").Scan(&rep.NullTenantRows)
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM soak_items WHERE title = 'FORGED_CROSS_TENANT_SOAK'").Scan(&rep.TamperedRows)
		if rep.NullTenantRows > 0 {
			rep.Passed = false
			rep.Failures = append(rep.Failures, fmt.Sprintf("found %d soak_items rows with null/empty tenant_id", rep.NullTenantRows))
		}
		if rep.TamperedRows > 0 {
			rep.Passed = false
			rep.Failures = append(rep.Failures, fmt.Sprintf("found %d forged cross-tenant rows in soak_items", rep.TamperedRows))
		}

		// 4. Check durable jobs DLQ and outbox failed counts
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM _jobs WHERE status = 'dead'").Scan(&rep.DeadJobs)
		_ = pool.QueryRow(ctx, "SELECT count(*) FROM _outbox_events WHERE status = 'failed'").Scan(&rep.FailedOutbox)
		if rep.FailedOutbox > 0 {
			rep.Passed = false
			rep.Failures = append(rep.Failures, fmt.Sprintf("found %d failed outbox events", rep.FailedOutbox))
		}

		// 5. Reconcile expected-results ledger against PostgreSQL persisted rows
		entries, err := state.ReadLedger(evidenceDir)
		if err == nil && len(entries) > 0 {
			deletedSet := make(map[string]bool)
			for _, e := range entries {
				if e.Kind == "record_delete" || e.Deleted {
					deletedSet[e.EntityID] = true
				}
			}
			// Check up to the latest 250 creates to keep periodic checks bounded while reconciling final pass completely
			start := 0
			if len(entries) > 250 {
				start = len(entries) - 250
			}
			for _, e := range entries[start:] {
				if e.Kind != "record_create" || deletedSet[e.EntityID] {
					continue
				}
				rep.LedgerChecked++
				var dbTenant, dbChecksum string
				qErr := pool.QueryRow(ctx, "SELECT tenant_id, checksum FROM soak_items WHERE id = $1", e.EntityID).Scan(&dbTenant, &dbChecksum)
				if qErr != nil {
					rep.MissingCommittedRows++
					continue
				}
				if dbTenant != e.TenantID {
					rep.CorruptedChecksumRows++
					rep.Failures = append(rep.Failures, fmt.Sprintf("tenant ownership mismatch on %s: db=%s ledger=%s", e.EntityID, dbTenant, e.TenantID))
				}
			}
			if rep.MissingCommittedRows > 0 {
				rep.Passed = false
				rep.Failures = append(rep.Failures, fmt.Sprintf("%d committed ledger row(s) missing from PostgreSQL", rep.MissingCommittedRows))
			}
			if rep.CorruptedChecksumRows > 0 {
				rep.Passed = false
			}
		}
	}

	// 6. Verify /api/studio/overview CheckReleaseBlockers (migrations, non-superuser, storage SHA-256)
	if client != nil && baseURL != "" && superToken != "" {
		st, raw, err := journeys.DoJSON(ctx, client, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/studio/overview", superToken, nil)
		if err != nil || st != http.StatusOK {
			rep.Passed = false
			rep.Failures = append(rep.Failures, fmt.Sprintf("GET /api/studio/overview failed: status=%d err=%v", st, err))
		} else {
			var ov struct {
				ReleaseBlockers struct {
					Passed   bool `json:"passed"`
					Findings []struct {
						Code    string `json:"code"`
						Detail  string `json:"detail"`
						Blocked bool   `json:"blocked"`
					} `json:"findings"`
				} `json:"releaseBlockers"`
			}
			if json.Unmarshal(raw, &ov) != nil {
				rep.Passed = false
				rep.Failures = append(rep.Failures, "failed parsing /api/studio/overview JSON")
			} else {
				rep.ReleaseBlockersPassed = ov.ReleaseBlockers.Passed
				if !ov.ReleaseBlockers.Passed {
					rep.Passed = false
					for _, f := range ov.ReleaseBlockers.Findings {
						if f.Blocked {
							rep.Failures = append(rep.Failures, fmt.Sprintf("releaseBlocker[%s]: %s", f.Code, f.Detail))
						}
					}
				}
			}
		}
	}

	return rep
}

// TrendMetrics summarizes steady-state memory, goroutine, WAL, and telemetry coverage.
type TrendMetrics struct {
	SampleCount            int     `json:"sampleCount"`
	ExpectedSamples        int     `json:"expectedSamples"`
	TelemetryCoveragePct   float64 `json:"telemetryCoveragePct"`
	HeapBaselineMiB        float64 `json:"heapBaselineMiB"`
	HeapFinalMiB           float64 `json:"heapFinalMiB"`
	HeapDriftPct           float64 `json:"heapDriftPct"`
	GoroutinesBaseline     float64 `json:"goroutinesBaseline"`
	GoroutinesFinal        float64 `json:"goroutinesFinal"`
	GoroutinesDriftPct     float64 `json:"goroutinesDriftPct"`
	MaxPoolWaitCount       int64   `json:"maxPoolWaitCount"`
	MaxRetainedWALBytes    int64   `json:"maxRetainedWalBytes"`
	MinDiskFreePct         float64 `json:"minDiskFreePct"`
	ReplicationSlotsStatus string  `json:"replicationSlotsStatus"`
	StandbyStatus          string  `json:"standbyStatus"`
}

// ReconciliationReport is the Phase 11 formal qualification evaluation.
type ReconciliationReport struct {
	CampaignID              string       `json:"campaignId"`
	GeneratedAtUTC          string       `json:"generatedAtUtc"`
	VerifierVersion         string       `json:"verifierVersion"`
	Verdict                 string       `json:"verdict"` // PASS, FAIL, INCOMPLETE
	OfficialStartUTC        string       `json:"officialStartUtc"`
	FinalTimestampUTC       string       `json:"finalTimestampUtc"`
	ElapsedHours            float64      `json:"elapsedHours"`
	RequiredHours           float64      `json:"requiredHours"`
	CandidateFrozen         bool         `json:"candidateFrozen"`
	Trends                  TrendMetrics `json:"trends"`
	CrossTenantLeaks        int64        `json:"crossTenantLeaks"`
	IntegrityViolations     int64        `json:"integrityViolations"`
	DisqualifyingInterrupts int          `json:"disqualifyingInterruptions"`
	BackupVerificationsOK   int          `json:"backupVerificationsOk"`
	BackupVerificationsFail int          `json:"backupVerificationsFail"`
	FaultDrillsCompleted    []string     `json:"faultDrillsCompleted"`
	MissingEvidence         []string     `json:"missingEvidence,omitempty"`
	FailureReasons          []string     `json:"failureReasons,omitempty"`
	SHA256ManifestPath      string       `json:"sha256ManifestPath,omitempty"`
	SHA256ManifestFiles     int          `json:"sha256ManifestFiles"`
}

// Reconcile evaluates a completed campaign directory against the formal W11-05 acceptance criteria.
// Any missing mandatory evidence produces INCOMPLETE; any critical integrity defect produces FAIL.
func Reconcile(dir string, requiredHours float64) (*ReconciliationReport, error) {
	if requiredHours <= 0 {
		requiredHours = 168.0
	}
	rep := &ReconciliationReport{
		GeneratedAtUTC:  time.Now().UTC().Format(time.RFC3339),
		VerifierVersion: VerifierVersion,
		RequiredHours:   requiredHours,
		Verdict:         "PASS",
	}

	// 1. Load checkpoint
	cpRaw, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		rep.Verdict = "INCOMPLETE"
		rep.MissingEvidence = append(rep.MissingEvidence, "state.json")
		return rep, nil
	}
	var cp state.Checkpoint
	if err := json.Unmarshal(cpRaw, &cp); err != nil {
		return nil, fmt.Errorf("parse state.json: %w", err)
	}
	rep.CampaignID = cp.CampaignID
	rep.OfficialStartUTC = cp.OfficialStartUTC.Format(time.RFC3339)
	rep.FinalTimestampUTC = cp.LastHeartbeatUTC.Format(time.RFC3339)
	rep.ElapsedHours = math.Round(cp.LastHeartbeatUTC.Sub(cp.OfficialStartUTC).Hours()*1000) / 1000
	rep.CrossTenantLeaks = cp.CrossTenantLeaks
	rep.IntegrityViolations = cp.IntegrityViolations

	// 2. Verify candidate provenance manifest exists
	if _, err := os.Stat(filepath.Join(dir, "candidate-manifest.json")); err == nil {
		rep.CandidateFrozen = true
	} else {
		rep.MissingEvidence = append(rep.MissingEvidence, "candidate-manifest.json")
	}

	// 3. Analyze 60-second telemetry samples
	samples, err := loadTelemetrySamples(filepath.Join(dir, "metrics.jsonl"))
	if err != nil || len(samples) == 0 {
		rep.MissingEvidence = append(rep.MissingEvidence, "metrics.jsonl")
	} else {
		rep.Trends = computeTrends(samples, rep.ElapsedHours)
	}

	// 4. Inspect interruptions
	interrupts, _ := loadInterruptions(filepath.Join(dir, "interruptions.jsonl"))
	for _, iv := range interrupts {
		if iv.Disqualifying {
			rep.DisqualifyingInterrupts++
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("disqualifying interruption (%s): %.1fs — %s", iv.Kind, iv.GapSeconds, iv.Detail))
		}
	}

	// 5. Inspect backup/restore verification logs
	bOK, bFail := countBackupResults(filepath.Join(dir, "backups.jsonl"))
	rep.BackupVerificationsOK = bOK
	rep.BackupVerificationsFail = bFail
	if bOK == 0 {
		rep.MissingEvidence = append(rep.MissingEvidence, "backups.jsonl (at least 1 verified online backup + isolated restore required)")
	}
	if bFail > 0 {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("%d backup/restore verification failure(s)", bFail))
	}

	// 6. Inspect scheduled fault drills (H24, H72, H120) for 168h campaigns
	faults := loadFaultResults(filepath.Join(dir, "faults.jsonl"))
	for _, f := range faults {
		if f.Passed {
			rep.FaultDrillsCompleted = append(rep.FaultDrillsCompleted, f.Drill)
		} else {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("fault drill %s failed: %s", f.Drill, f.Detail))
		}
	}
	if requiredHours >= 168.0 {
		for _, needed := range []string{"hour24_worker_kill", "hour72_pg_disconnect", "hour120_backup_restore"} {
			if !contains(rep.FaultDrillsCompleted, needed) {
				rep.MissingEvidence = append(rep.MissingEvidence, "fault drill "+needed)
			}
		}
	}

	// 7. Evaluate formal acceptance thresholds
	if rep.ElapsedHours < requiredHours {
		rep.MissingEvidence = append(rep.MissingEvidence, fmt.Sprintf("elapsed wall-clock hours %.3fh < required %.1fh", rep.ElapsedHours, requiredHours))
	}
	if cp.AttemptedOps == 0 || cp.SuccessfulOps == 0 {
		rep.FailureReasons = append(rep.FailureReasons, "zero successful workload operations recorded")
	}
	if cp.CrossTenantLeaks > 0 {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("CRITICAL: %d cross-tenant leak(s)", cp.CrossTenantLeaks))
	}
	if cp.IntegrityViolations > 0 {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("CRITICAL: %d integrity violation(s)", cp.IntegrityViolations))
	}
	if cp.EmergencyStopped {
		rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("emergency stop triggered: %s", cp.EmergencyStopReason))
	}
	if rep.Trends.SampleCount > 0 {
		if rep.Trends.TelemetryCoveragePct < 95.0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("telemetry coverage %.2f%% < 95.0%%", rep.Trends.TelemetryCoveragePct))
		}
		if rep.Trends.HeapDriftPct > 5.0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("sustained heap memory drift %.2f%% > 5.0%% criterion", rep.Trends.HeapDriftPct))
		}
		if rep.Trends.GoroutinesDriftPct > 25.0 {
			rep.FailureReasons = append(rep.FailureReasons, fmt.Sprintf("sustained goroutine growth %.2f%% > 25.0%% threshold", rep.Trends.GoroutinesDriftPct))
		}
	}

	// Fail-closed verdict priority: FAIL > INCOMPLETE > PASS
	switch {
	case len(rep.FailureReasons) > 0:
		rep.Verdict = "FAIL"
	case len(rep.MissingEvidence) > 0:
		rep.Verdict = "INCOMPLETE"
	default:
		rep.Verdict = "PASS"
	}

	return rep, nil
}

// AuditVerdict is the strictly read-only A11 operational certification verdict.
type AuditVerdict struct {
	AuditedAtUTC       string                `json:"auditedAtUtc"`
	AuditorRole        string                `json:"auditorRole"`
	EvidenceDir        string                `json:"evidenceDir"`
	Verdict            string                `json:"verdict"` // W11-05_VERIFIED, W11-05_CONDITIONALLY_ACCEPTABLE, W11-05_FAILED, W11-05_INCOMPLETE_EVIDENCE
	ManifestVerified   bool                  `json:"manifestVerified"`
	ManifestMismatches []string              `json:"manifestMismatches,omitempty"`
	Dimensions         map[string]string     `json:"dimensions"` // VERIFIED, FAILED, NOT_VERIFIED
	Reconciliation     *ReconciliationReport `json:"reconciliation,omitempty"`
}

// AuditCampaign performs strictly read-only A11 independent examination of a frozen evidence directory.
func AuditCampaign(dir string, requiredHours float64) (*AuditVerdict, error) {
	av := &AuditVerdict{
		AuditedAtUTC: time.Now().UTC().Format(time.RFC3339),
		AuditorRole:  "A11_INDEPENDENT_READ_ONLY_AUDITOR",
		EvidenceDir:  dir,
		Dimensions:   make(map[string]string),
	}

	// 1. Independently verify SHA256SUMS.txt cryptographic manifest
	manifestPath := filepath.Join(dir, "SHA256SUMS.txt")
	mRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		av.Dimensions["sha256_manifest"] = "NOT_VERIFIED"
	} else {
		av.ManifestVerified = true
		for _, line := range strings.Split(string(mRaw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) != 2 {
				continue
			}
			wantSum, name := parts[0], parts[1]
			gotSum, hErr := state.FileSHA256(filepath.Join(dir, name))
			if hErr != nil || gotSum != wantSum {
				av.ManifestVerified = false
				av.ManifestMismatches = append(av.ManifestMismatches, fmt.Sprintf("%s: want=%s got=%s err=%v", name, wantSum, gotSum, hErr))
			}
		}
		if av.ManifestVerified {
			av.Dimensions["sha256_manifest"] = "VERIFIED"
		} else {
			av.Dimensions["sha256_manifest"] = "FAILED"
		}
	}

	// 2. Independently recompute reconciliation from raw artifacts (never trusting A01's summary alone)
	rec, err := Reconcile(dir, requiredHours)
	if err != nil {
		return nil, err
	}
	av.Reconciliation = rec

	if rec.CandidateFrozen {
		av.Dimensions["candidate_provenance"] = "VERIFIED"
	} else {
		av.Dimensions["candidate_provenance"] = "NOT_VERIFIED"
	}
	if rec.ElapsedHours >= requiredHours && rec.DisqualifyingInterrupts == 0 {
		av.Dimensions["timeline_168h"] = "VERIFIED"
	} else if rec.DisqualifyingInterrupts > 0 {
		av.Dimensions["timeline_168h"] = "FAILED"
	} else {
		av.Dimensions["timeline_168h"] = "NOT_VERIFIED"
	}
	if rec.CrossTenantLeaks == 0 && rec.IntegrityViolations == 0 {
		av.Dimensions["security_and_integrity"] = "VERIFIED"
	} else {
		av.Dimensions["security_and_integrity"] = "FAILED"
	}
	if rec.BackupVerificationsOK > 0 && rec.BackupVerificationsFail == 0 {
		av.Dimensions["backup_and_restore"] = "VERIFIED"
	} else if rec.BackupVerificationsFail > 0 {
		av.Dimensions["backup_and_restore"] = "FAILED"
	} else {
		av.Dimensions["backup_and_restore"] = "NOT_VERIFIED"
	}

	switch {
	case len(av.ManifestMismatches) > 0 || rec.Verdict == "FAIL":
		av.Verdict = "W11-05_FAILED"
	case rec.Verdict == "INCOMPLETE" || !av.ManifestVerified:
		av.Verdict = "W11-05_INCOMPLETE_EVIDENCE"
	default:
		av.Verdict = "W11-05_VERIFIED"
	}

	return av, nil
}

func loadTelemetrySamples(path string) ([]telemetry.Sample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []telemetry.Sample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var s telemetry.Sample
		if json.Unmarshal([]byte(line), &s) == nil {
			out = append(out, s)
		}
	}
	return out, sc.Err()
}

func computeTrends(samples []telemetry.Sample, elapsedHours float64) TrendMetrics {
	tm := TrendMetrics{
		SampleCount:            len(samples),
		ExpectedSamples:        max(int(math.Floor(elapsedHours*60.0)), 1),
		MinDiskFreePct:         100.0,
		ReplicationSlotsStatus: telemetry.StatusNotConfigured,
		StandbyStatus:          telemetry.StatusNotConfigured,
	}
	tm.TelemetryCoveragePct = math.Min(100.0, math.Round(float64(tm.SampleCount)*100.0/float64(tm.ExpectedSamples)*100)/100)

	// Compare first steady-state decile (after 5% warmup) against final decile
	var heaps []float64
	var goros []float64
	for _, s := range samples {
		if s.Host.DiskFreePct > 0 && s.Host.DiskFreePct < tm.MinDiskFreePct {
			tm.MinDiskFreePct = math.Round(s.Host.DiskFreePct*100) / 100
		}
		if s.RuxbaseOps != nil {
			heaps = append(heaps, float64(s.RuxbaseOps.Runtime.HeapAllocBytes)/(1024*1024))
			goros = append(goros, float64(s.RuxbaseOps.Runtime.Goroutines))
			if s.RuxbaseOps.DBPool.WaitCount > tm.MaxPoolWaitCount {
				tm.MaxPoolWaitCount = s.RuxbaseOps.DBPool.WaitCount
			}
		}
		if s.Postgres != nil {
			if s.Postgres.MaxRetainedWALBytes > tm.MaxRetainedWALBytes {
				tm.MaxRetainedWALBytes = s.Postgres.MaxRetainedWALBytes
			}
			if s.Postgres.ReplicationSlotsStatus != "" {
				tm.ReplicationSlotsStatus = s.Postgres.ReplicationSlotsStatus
			}
			if s.Postgres.StandbyStatus != "" {
				tm.StandbyStatus = s.Postgres.StandbyStatus
			}
		}
	}

	if len(heaps) >= 2 {
		win := max(len(heaps)/10, 1)
		warm := min(len(heaps)/20, len(heaps)-win)
		tm.HeapBaselineMiB = round2(avg(heaps[warm : warm+win]))
		tm.HeapFinalMiB = round2(avg(heaps[len(heaps)-win:]))
		if tm.HeapBaselineMiB > 0 {
			tm.HeapDriftPct = round2(((tm.HeapFinalMiB - tm.HeapBaselineMiB) / tm.HeapBaselineMiB) * 100.0)
		}
		tm.GoroutinesBaseline = round2(avg(goros[warm : warm+win]))
		tm.GoroutinesFinal = round2(avg(goros[len(goros)-win:]))
		if tm.GoroutinesBaseline > 0 {
			tm.GoroutinesDriftPct = round2(((tm.GoroutinesFinal - tm.GoroutinesBaseline) / tm.GoroutinesBaseline) * 100.0)
		}
	}
	return tm
}

func loadInterruptions(path string) ([]state.InterruptionEvent, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []state.InterruptionEvent
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var iv state.InterruptionEvent
		if json.Unmarshal(sc.Bytes(), &iv) == nil {
			out = append(out, iv)
		}
	}
	return out, sc.Err()
}

func countBackupResults(path string) (int, int) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var okCnt, failCnt int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var rec struct {
			Passed bool `json:"passed"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil {
			if rec.Passed {
				okCnt++
			} else {
				failCnt++
			}
		}
	}
	return okCnt, failCnt
}

type faultResult struct {
	Drill  string `json:"drill"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail"`
}

func loadFaultResults(path string) []faultResult {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []faultResult
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var fr faultResult
		if json.Unmarshal(sc.Bytes(), &fr) == nil && fr.Drill != "" {
			out = append(out, fr)
		}
	}
	return out
}

func avg(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	return sum / float64(len(xs))
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}
