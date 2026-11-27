package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"runtime"
	"runtime/metrics"
	"strconv"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/core/cluster"
	"github.com/pocketbase/dbx"
)

// OpsRuntimeMetrics captures Go process runtime memory and scheduling stats.
type OpsRuntimeMetrics struct {
	Goroutines        int    `json:"goroutines"`
	HeapAllocBytes    uint64 `json:"heapAllocBytes"`
	LiveHeapBytes     uint64 `json:"liveHeapBytes"`
	HeapInuseBytes    uint64 `json:"heapInuseBytes"`
	HeapSysBytes      uint64 `json:"heapSysBytes"`
	HeapReleasedBytes uint64 `json:"heapReleasedBytes"`
	HeapObjects       uint64 `json:"heapObjects"`
	NumGC             uint32 `json:"numGC"`
	PauseTotalNs      uint64 `json:"pauseTotalNs"`
	RSSBytes          uint64 `json:"rssBytes,omitempty"`
	OpenFDs           int    `json:"openFds,omitempty"`
}

// OpsDBPoolMetrics captures Go SQL connection pool statistics.
type OpsDBPoolMetrics struct {
	MaxOpenConnections int   `json:"maxOpenConnections"`
	OpenConnections    int   `json:"openConnections"`
	InUse              int   `json:"inUse"`
	Idle               int   `json:"idle"`
	WaitCount          int64 `json:"waitCount"`
	WaitDurationMs     int64 `json:"waitDurationMs"`
}

// OpsDatabaseMetrics captures PostgreSQL database footprint and active connections.
type OpsDatabaseMetrics struct {
	SizeBytes      int64 `json:"sizeBytes"`
	ActiveBackends int   `json:"activeBackends"`
}

// OpsJobsMetrics captures durable queue and transactional outbox backlog counts.
type OpsJobsMetrics struct {
	Pending       int `json:"pending"`
	Running       int `json:"running"`
	Failed        int `json:"failed"`
	Dead          int `json:"dead"`
	OutboxPending int `json:"outboxPending"`
	OutboxFailed  int `json:"outboxFailed"`
}

// OpsClusterMetrics captures cluster synchronization and WAL retention metrics.
type OpsClusterMetrics struct {
	Enabled           bool   `json:"enabled"`
	Connected         bool   `json:"connected"`
	ReplicationLeader bool   `json:"replicationLeader"`
	RealtimeSource    string `json:"realtimeSource"`
	RetainedWALBytes  int64  `json:"retainedWALBytes"`
}

// OpsMetricsSnapshot represents a point-in-time observability snapshot.
type OpsMetricsSnapshot struct {
	Timestamp string             `json:"timestamp"`
	NodeId    string             `json:"nodeId"`
	Runtime   OpsRuntimeMetrics  `json:"runtime"`
	DBPool    OpsDBPoolMetrics   `json:"dbPool"`
	Database  OpsDatabaseMetrics `json:"database"`
	Jobs      OpsJobsMetrics     `json:"jobs"`
	Cluster   OpsClusterMetrics  `json:"cluster"`
}

// OpsAlert represents an actionable operational condition.
type OpsAlert struct {
	Code        string `json:"code"`
	Severity    string `json:"severity"` // info, warning, critical
	Message     string `json:"message"`
	Remediation string `json:"remediation"`
}

// CapacityBudgetReport summarizes connection budgets and measured runtime memory footprints.
type CapacityBudgetReport struct {
	Profile              string `json:"profile"`
	MainPoolMax          int    `json:"mainPoolMax"`
	AuxPoolMax           int    `json:"auxPoolMax"`
	ListenConns          int    `json:"listenConns"`
	ReplicationConns     int    `json:"replicationConns"`
	TotalPerNodeMax      int    `json:"totalPerNodeMax"`
	ServerMaxConnections int    `json:"serverMaxConnections"`
	Standard3NodeTotal   int    `json:"standard3NodeTotal"`
	WithinServerBudget   bool   `json:"withinServerBudget"`
	MeasuredHeapBytes    uint64 `json:"measuredHeapBytes"`
}

// ReleaseBlockerFinding describes a P0 release-blocking defect if discovered.
type ReleaseBlockerFinding struct {
	Code    string `json:"code"`
	Detail  string `json:"detail"`
	Blocked bool   `json:"blocked"`
}

// ReleaseBlockerReport aggregates release safety verifications (W08-07).
type ReleaseBlockerReport struct {
	Passed   bool                    `json:"passed"`
	Findings []ReleaseBlockerFinding `json:"findings"`
}

// CollectOpsMetrics gathers live runtime, database pool, PostgreSQL, jobs, and cluster metrics.
func CollectOpsMetrics(ctx context.Context, app App) (*OpsMetricsSnapshot, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	liveHeap := readLiveHeapBytes()
	if liveHeap == 0 {
		liveHeap = mem.HeapAlloc
	}
	rssBytes, openFDs := readProcessResourceStats()

	snap := &OpsMetricsSnapshot{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		NodeId:    app.Cluster().NodeId(),
		Runtime: OpsRuntimeMetrics{
			Goroutines:        runtime.NumGoroutine(),
			HeapAllocBytes:    mem.HeapAlloc,
			LiveHeapBytes:     liveHeap,
			HeapInuseBytes:    mem.HeapInuse,
			HeapSysBytes:      mem.HeapSys,
			HeapReleasedBytes: mem.HeapReleased,
			HeapObjects:       mem.HeapObjects,
			NumGC:             mem.NumGC,
			PauseTotalNs:      mem.PauseTotalNs,
			RSSBytes:          rssBytes,
			OpenFDs:           openFDs,
		},
		Cluster: OpsClusterMetrics{
			Enabled:           app.Cluster().Enabled(),
			Connected:         app.Cluster().Connected(),
			ReplicationLeader: app.Cluster().IsReplicationLeader(),
			RealtimeSource:    app.Cluster().RealtimeSource(),
		},
	}

	if db, ok := app.DB().(*dbx.DB); ok && db != nil {
		stats := db.DB().Stats()
		snap.DBPool = OpsDBPoolMetrics{
			MaxOpenConnections: stats.MaxOpenConnections,
			OpenConnections:    stats.OpenConnections,
			InUse:              stats.InUse,
			Idle:               stats.Idle,
			WaitCount:          stats.WaitCount,
			WaitDurationMs:     stats.WaitDuration.Milliseconds(),
		}
	}

	_ = app.DB().NewQuery("SELECT pg_database_size(current_database())").WithContext(ctx).Row(&snap.Database.SizeBytes)
	_ = app.DB().NewQuery("SELECT count(*) FROM pg_stat_activity WHERE datname = current_database()").WithContext(ctx).Row(&snap.Database.ActiveBackends)

	var jobRows []struct {
		Status string `db:"status"`
		Count  int    `db:"cnt"`
	}
	if err := app.DB().NewQuery("SELECT [[status]], count(*) AS [[cnt]] FROM {{_jobs}} GROUP BY [[status]]").WithContext(ctx).All(&jobRows); err == nil {
		for _, r := range jobRows {
			switch r.Status {
			case JobStatusPending:
				snap.Jobs.Pending = r.Count
			case JobStatusRunning:
				snap.Jobs.Running = r.Count
			case JobStatusFailed:
				snap.Jobs.Failed = r.Count
			case JobStatusDead:
				snap.Jobs.Dead = r.Count
			}
		}
	}

	var outboxRows []struct {
		Status string `db:"status"`
		Count  int    `db:"cnt"`
	}
	if err := app.DB().NewQuery("SELECT [[status]], count(*) AS [[cnt]] FROM {{_outbox_events}} GROUP BY [[status]]").WithContext(ctx).All(&outboxRows); err == nil {
		for _, r := range outboxRows {
			switch r.Status {
			case OutboxStatusPending:
				snap.Jobs.OutboxPending = r.Count
			case OutboxStatusFailed:
				snap.Jobs.OutboxFailed = r.Count
			}
		}
	}

	if slotMetrics, err := app.Cluster().SlotMetrics(ctx); err == nil && slotMetrics != nil {
		snap.Cluster.RetainedWALBytes = slotMetrics.RetainedBytes
	}

	return snap, nil
}

// FormatPrometheusMetrics serializes an OpsMetricsSnapshot in Prometheus text exposition format.
func FormatPrometheusMetrics(snap *OpsMetricsSnapshot) string {
	var b strings.Builder

	writeGauge := func(name, help string, val any) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n%s %v\n", name, help, name, name, val)
	}

	writeGauge("ruxbase_runtime_goroutines", "Current number of goroutines.", snap.Runtime.Goroutines)
	writeGauge("ruxbase_runtime_heap_alloc_bytes", "Bytes of allocated heap objects.", snap.Runtime.HeapAllocBytes)
	writeGauge("ruxbase_runtime_live_heap_bytes", "Bytes of live heap objects marked by the most recent completed GC.", snap.Runtime.LiveHeapBytes)
	writeGauge("ruxbase_runtime_heap_inuse_bytes", "Bytes in in-use heap spans.", snap.Runtime.HeapInuseBytes)
	writeGauge("ruxbase_runtime_heap_sys_bytes", "Bytes of heap memory obtained from the OS.", snap.Runtime.HeapSysBytes)
	writeGauge("ruxbase_runtime_num_gc", "Total number of completed GC cycles.", snap.Runtime.NumGC)
	writeGauge("ruxbase_runtime_gc_pause_total_ns", "Cumulative GC stop-the-world pause nanoseconds.", snap.Runtime.PauseTotalNs)
	writeGauge("ruxbase_runtime_rss_bytes", "Process resident set size in bytes (when available).", snap.Runtime.RSSBytes)
	writeGauge("ruxbase_runtime_open_fds", "Current open file descriptors (when available).", snap.Runtime.OpenFDs)
	writeGauge("ruxbase_db_pool_max_open", "Configured maximum open DB connections.", snap.DBPool.MaxOpenConnections)
	writeGauge("ruxbase_db_pool_in_use", "DB connections currently in use.", snap.DBPool.InUse)
	writeGauge("ruxbase_db_pool_idle", "Idle DB connections in pool.", snap.DBPool.Idle)
	writeGauge("ruxbase_db_pool_wait_count", "Total number of connections waited for.", snap.DBPool.WaitCount)
	writeGauge("ruxbase_db_size_bytes", "Current PostgreSQL database size in bytes.", snap.Database.SizeBytes)
	writeGauge("ruxbase_db_active_backends", "Active backends connected to this database.", snap.Database.ActiveBackends)
	writeGauge("ruxbase_jobs_pending", "Pending durable jobs.", snap.Jobs.Pending)
	writeGauge("ruxbase_jobs_failed", "Failed jobs awaiting backoff retry.", snap.Jobs.Failed)
	writeGauge("ruxbase_jobs_dead", "Dead-letter queue jobs requiring operator review.", snap.Jobs.Dead)
	writeGauge("ruxbase_outbox_pending", "Pending transactional outbox events.", snap.Jobs.OutboxPending)
	writeGauge("ruxbase_outbox_failed", "Failed transactional outbox events.", snap.Jobs.OutboxFailed)
	writeGauge("ruxbase_cluster_retained_wal_bytes", "PostgreSQL replication slot retained WAL bytes.", snap.Cluster.RetainedWALBytes)

	return b.String()
}

// EvaluateOpsAlerts evaluates actionable operational alerts from the current metrics snapshot.
func EvaluateOpsAlerts(snap *OpsMetricsSnapshot) []OpsAlert {
	alerts := []OpsAlert{}

	if snap.Jobs.Dead > 0 {
		alerts = append(alerts, OpsAlert{
			Code:        "dlq_non_empty",
			Severity:    "warning",
			Message:     fmt.Sprintf("%d job(s) have exhausted retries and entered the Dead Letter Queue", snap.Jobs.Dead),
			Remediation: "Inspect failing jobs in _jobs (status='dead') and replay fixed tasks via ReplayDeadJob.",
		})
	}

	if snap.Jobs.OutboxFailed > 0 {
		alerts = append(alerts, OpsAlert{
			Code:        "outbox_failed",
			Severity:    "warning",
			Message:     fmt.Sprintf("%d transactional outbox event(s) failed delivery", snap.Jobs.OutboxFailed),
			Remediation: "Inspect _outbox_events (status='failed') and verify downstream webhook/event receivers.",
		})
	}

	if snap.DBPool.WaitCount > 0 && snap.DBPool.WaitDurationMs > 1000 {
		alerts = append(alerts, OpsAlert{
			Code:        "db_pool_wait",
			Severity:    "warning",
			Message:     fmt.Sprintf("Database pool experienced %d waits totaling %dms", snap.DBPool.WaitCount, snap.DBPool.WaitDurationMs),
			Remediation: "Review slow queries or increase RUXBASE_DB_MAX_CONNS if PostgreSQL server CPU allows.",
		})
	}

	if snap.Cluster.RetainedWALBytes > cluster.RetainedWALWarningSize {
		alerts = append(alerts, OpsAlert{
			Code:        "wal_retention_high",
			Severity:    "warning",
			Message:     fmt.Sprintf("Replication slot WAL retention (%d bytes) exceeds warning threshold", snap.Cluster.RetainedWALBytes),
			Remediation: "Verify replication leader liveness and pg_replication_slots active state.",
		})
	}

	if snap.Cluster.Enabled && !snap.Cluster.Connected {
		alerts = append(alerts, OpsAlert{
			Code:        "cluster_listen_down",
			Severity:    "critical",
			Message:     "Cluster LISTEN connection is disconnected; node is isolated from cross-node invalidations",
			Remediation: "Check direct PostgreSQL connectivity (avoid transaction poolers on LISTEN connection).",
		})
	}

	return alerts
}

// EvaluateCapacityBudget computes node and 3-node cluster connection budgets against PostgreSQL limits.
func EvaluateCapacityBudget(ctx context.Context, app App) (*CapacityBudgetReport, error) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	mainMax := 20
	if db, ok := app.DB().(*dbx.DB); ok && db != nil {
		if configured := db.DB().Stats().MaxOpenConnections; configured > 0 {
			mainMax = configured
		}
	}

	auxMax := 5
	if auxDB, ok := app.AuxDB().(*dbx.DB); ok && auxDB != nil {
		if configured := auxDB.DB().Stats().MaxOpenConnections; configured > 0 {
			auxMax = configured
		}
	}

	listenConns := 1
	replConns := 0
	if app.Cluster().RealtimeSource() == "replication" {
		replConns = 2
	}

	perNode := mainMax + auxMax + listenConns
	standard3Node := (perNode * 3) + replConns

	serverMax := 100
	var maxConnsStr string
	if err := app.DB().NewQuery("SHOW max_connections").WithContext(ctx).Row(&maxConnsStr); err == nil {
		if parsed, convErr := strconv.Atoi(strings.TrimSpace(maxConnsStr)); convErr == nil && parsed > 0 {
			serverMax = parsed
		}
	}

	profile := "Lite (Single Node)"
	if app.Cluster().Enabled() {
		profile = "Standard (Multi-Node App Cluster)"
	}

	return &CapacityBudgetReport{
		Profile:              profile,
		MainPoolMax:          mainMax,
		AuxPoolMax:           auxMax,
		ListenConns:          listenConns,
		ReplicationConns:     replConns,
		TotalPerNodeMax:      perNode + replConns,
		ServerMaxConnections: serverMax,
		Standard3NodeTotal:   standard3Node,
		WithinServerBudget:   standard3Node <= serverMax,
		MeasuredHeapBytes:    mem.HeapAlloc,
	}, nil
}

// CheckReleaseBlockers verifies P0 operational and security gates (role isolation,
// migration history consistency, and storage object checksum integrity).
func CheckReleaseBlockers(ctx context.Context, app App) (*ReleaseBlockerReport, error) {
	report := &ReleaseBlockerReport{
		Passed:   true,
		Findings: []ReleaseBlockerFinding{},
	}

	// 1. P0 Check: Runtime PostgreSQL role must not hold SUPERUSER or BYPASSRLS
	var roleInfo struct {
		RoleName  string `db:"rolname"`
		Super     bool   `db:"rolsuper"`
		BypassRLS bool   `db:"rolbypassrls"`
	}
	err := app.DB().NewQuery(`
		SELECT [[rolname]], [[rolsuper]], [[rolbypassrls]]
		FROM pg_roles
		WHERE [[rolname]] = current_user
	`).WithContext(ctx).One(&roleInfo)
	if err != nil {
		report.Passed = false
		report.Findings = append(report.Findings, ReleaseBlockerFinding{
			Code:    "role_inspection_failed",
			Detail:  fmt.Sprintf("unable to verify runtime role privileges: %v", err),
			Blocked: true,
		})
	} else if roleInfo.Super || roleInfo.BypassRLS {
		report.Passed = false
		report.Findings = append(report.Findings, ReleaseBlockerFinding{
			Code:    "privileged_runtime_role",
			Detail:  fmt.Sprintf("runtime role %q holds SUPERUSER=%v or BYPASSRLS=%v; least-privilege violation", roleInfo.RoleName, roleInfo.Super, roleInfo.BypassRLS),
			Blocked: true,
		})
	} else {
		report.Findings = append(report.Findings, ReleaseBlockerFinding{
			Code:    "least_privilege_runtime_role",
			Detail:  fmt.Sprintf("runtime role %q strictly non-superuser without BYPASSRLS", roleInfo.RoleName),
			Blocked: false,
		})
	}

	// 2. P0 Check: All registered SystemMigrations must be applied cleanly in _migrations
	var appliedFiles []string
	if err := app.DB().NewQuery("SELECT [[file]] FROM {{_migrations}}").WithContext(ctx).Column(&appliedFiles); err != nil {
		report.Passed = false
		report.Findings = append(report.Findings, ReleaseBlockerFinding{
			Code:    "migrations_ledger_unreadable",
			Detail:  fmt.Sprintf("failed reading _migrations ledger: %v", err),
			Blocked: true,
		})
	} else {
		appliedSet := make(map[string]struct{}, len(appliedFiles))
		for _, f := range appliedFiles {
			appliedSet[f] = struct{}{}
		}
		var missing []string
		for _, m := range SystemMigrations.Items() {
			if _, ok := appliedSet[m.File]; !ok {
				missing = append(missing, m.File)
			}
		}
		if len(missing) > 0 {
			report.Passed = false
			report.Findings = append(report.Findings, ReleaseBlockerFinding{
				Code:    "unapplied_system_migrations",
				Detail:  fmt.Sprintf("missing system migrations: %s", strings.Join(missing, ", ")),
				Blocked: true,
			})
		} else {
			report.Findings = append(report.Findings, ReleaseBlockerFinding{
				Code:    "system_migrations_consistent",
				Detail:  fmt.Sprintf("all %d system migrations applied", len(SystemMigrations.Items())),
				Blocked: false,
			})
		}
	}

	// 3. P0 Check: Storage object checksum & file integrity vs _storage_objects metadata
	var objects []struct {
		BucketId string `db:"bucket_id"`
		Name     string `db:"name"`
		Etag     string `db:"etag"`
	}
	if err := app.DB().NewQuery("SELECT [[bucket_id]], [[name]], [[etag]] FROM {{_storage_objects}}").WithContext(ctx).All(&objects); err == nil && len(objects) > 0 {
		fsys, fsErr := app.NewFilesystem()
		if fsErr != nil {
			report.Passed = false
			report.Findings = append(report.Findings, ReleaseBlockerFinding{
				Code:    "storage_filesystem_unavailable",
				Detail:  fmt.Sprintf("failed to open storage filesystem for integrity check: %v", fsErr),
				Blocked: true,
			})
		} else {
			defer fsys.Close()
			var corrupted []string
			for _, obj := range objects {
				key := path.Join("_storage", obj.BucketId, strings.TrimLeft(path.Clean(obj.Name), "/"))
				reader, rErr := fsys.GetReader(key)
				if rErr != nil {
					corrupted = append(corrupted, fmt.Sprintf("%s (missing blob)", obj.Name))
					continue
				}
				hasher := sha256.New()
				_, copyErr := io.Copy(hasher, reader)
				reader.Close()
				if copyErr != nil {
					corrupted = append(corrupted, fmt.Sprintf("%s (read error)", obj.Name))
					continue
				}
				actualEtag := hex.EncodeToString(hasher.Sum(nil))
				if obj.Etag != "" && actualEtag != obj.Etag {
					corrupted = append(corrupted, fmt.Sprintf("%s (checksum mismatch)", obj.Name))
				}
			}
			if len(corrupted) > 0 {
				report.Passed = false
				report.Findings = append(report.Findings, ReleaseBlockerFinding{
					Code:    "storage_integrity_violation",
					Detail:  fmt.Sprintf("corrupted or missing storage objects: %s", strings.Join(corrupted, ", ")),
					Blocked: true,
				})
			} else {
				report.Findings = append(report.Findings, ReleaseBlockerFinding{
					Code:    "storage_integrity_verified",
					Detail:  fmt.Sprintf("verified SHA-256 checksums for %d storage object(s)", len(objects)),
					Blocked: false,
				})
			}
		}
	}

	return report, nil
}

// readLiveHeapBytes queries Go runtime/metrics for /gc/heap/live:bytes (post-GC live heap,
// immune to intra-cycle allocation sawtooth variance).
func readLiveHeapBytes() uint64 {
	samples := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
	metrics.Read(samples)
	if samples[0].Value.Kind() == metrics.KindUint64 {
		return samples[0].Value.Uint64()
	}
	return 0
}

// readProcessResourceStats reads Linux /proc/self/statm RSS and /proc/self/fd count
// when available, returning zeros gracefully on unsupported platforms.
func readProcessResourceStats() (rssBytes uint64, openFDs int) {
	if raw, err := os.ReadFile("/proc/self/statm"); err == nil {
		fields := strings.Fields(string(raw))
		if len(fields) >= 2 {
			if pages, convErr := strconv.ParseUint(fields[1], 10, 64); convErr == nil {
				rssBytes = pages * uint64(os.Getpagesize())
			}
		}
	}
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		openFDs = len(entries)
	}
	return rssBytes, openFDs
}
