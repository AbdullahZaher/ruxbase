// Package telemetry collects 60-second PostgreSQL, WAL/replication, Ruxbase runtime,
// and host resource metrics for the W11-05 168-hour staging soak.
//
// When no replication slot or replica is configured, replication fields are explicitly
// labeled "NOT_CONFIGURED" rather than falsely reporting zero lag.
package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const StatusNotConfigured = "NOT_CONFIGURED"

// Sample is a complete 60-second point-in-time telemetry snapshot persisted to metrics.jsonl.
type Sample struct {
	TimestampUTC       string              `json:"timestampUtc"`
	ElapsedSec         float64             `json:"elapsedSec"`
	CollectorError     string              `json:"collectorError,omitempty"`
	HealthReadyStatus  int                 `json:"healthReadyStatus"`
	HealthReadyOK      bool                `json:"healthReadyOk"`
	RuxbaseOps         *RuxbaseOpsSnapshot `json:"ruxbaseOps,omitempty"`
	RuxbaseAlertsCount int                 `json:"ruxbaseAlertsCount"`
	RuxbaseAlerts      []map[string]any    `json:"ruxbaseAlerts,omitempty"`
	Postgres           *PostgresMetrics    `json:"postgres,omitempty"`
	Host               HostMetrics         `json:"host"`
}

// RuxbaseOpsSnapshot mirrors GET /api/ops/metrics.
type RuxbaseOpsSnapshot struct {
	Timestamp string `json:"timestamp"`
	NodeId    string `json:"nodeId"`
	Runtime   struct {
		Goroutines     int    `json:"goroutines"`
		HeapAllocBytes uint64 `json:"heapAllocBytes"`
		HeapSysBytes   uint64 `json:"heapSysBytes"`
		NumGC          uint32 `json:"numGC"`
	} `json:"runtime"`
	DBPool struct {
		MaxOpenConnections int   `json:"maxOpenConnections"`
		OpenConnections    int   `json:"openConnections"`
		InUse              int   `json:"inUse"`
		Idle               int   `json:"idle"`
		WaitCount          int64 `json:"waitCount"`
		WaitDurationMs     int64 `json:"waitDurationMs"`
	} `json:"dbPool"`
	Database struct {
		SizeBytes      int64 `json:"sizeBytes"`
		ActiveBackends int   `json:"activeBackends"`
	} `json:"database"`
	Jobs struct {
		Pending       int `json:"pending"`
		Running       int `json:"running"`
		Failed        int `json:"failed"`
		Dead          int `json:"dead"`
		OutboxPending int `json:"outboxPending"`
		OutboxFailed  int `json:"outboxFailed"`
	} `json:"jobs"`
	Cluster struct {
		Enabled           bool   `json:"enabled"`
		Connected         bool   `json:"connected"`
		ReplicationLeader bool   `json:"replicationLeader"`
		RealtimeSource    string `json:"realtimeSource"`
		RetainedWALBytes  int64  `json:"retainedWALBytes"`
	} `json:"cluster"`
}

// PostgresMetrics captures PostgreSQL 17/18 catalog/statistics views.
type PostgresMetrics struct {
	ServerVersion          string           `json:"serverVersion"`
	DatabaseSizeBytes      int64            `json:"databaseSizeBytes"`
	ActiveConns            int              `json:"activeConns"`
	IdleConns              int              `json:"idleConns"`
	IdleInTxConns          int              `json:"idleInTxConns"`
	WaitingLocks           int              `json:"waitingLocks"`
	LongestTxSec           float64          `json:"longestTxSec"`
	XactCommit             int64            `json:"xactCommit"`
	XactRollback           int64            `json:"xactRollback"`
	BlksRead               int64            `json:"blksRead"`
	BlksHit                int64            `json:"blksHit"`
	CacheHitRatioPct       float64          `json:"cacheHitRatioPct"`
	Deadlocks              int64            `json:"deadlocks"`
	TempFiles              int64            `json:"tempFiles"`
	TempBytes              int64            `json:"tempBytes"`
	SeqScans               int64            `json:"seqScans"`
	IdxScans               int64            `json:"idxScans"`
	LiveTuples             int64            `json:"liveTuples"`
	DeadTuples             int64            `json:"deadTuples"`
	AutovacuumCount        int64            `json:"autovacuumCount"`
	MaxTxidAge             int64            `json:"maxTxidAge"`
	CheckpointsTimed       int64            `json:"checkpointsTimed"`
	CheckpointsRequested   int64            `json:"checkpointsRequested"`
	CheckpointWriteTimeMs  float64          `json:"checkpointWriteTimeMs"`
	WALLSN                 string           `json:"walLsn"`
	WALRecords             int64            `json:"walRecords"`
	WALBytes               int64            `json:"walBytes"`
	WALArchiveFailedCount  int64            `json:"walArchiveFailedCount"`
	ReplicationSlotsStatus string           `json:"replicationSlotsStatus"` // CONFIGURED or NOT_CONFIGURED
	ReplicationSlots       []ReplSlotMetric `json:"replicationSlots,omitempty"`
	StandbyStatus          string           `json:"standbyStatus"` // CONFIGURED or NOT_CONFIGURED
	StandbyReplicas        []StandbyMetric  `json:"standbyReplicas,omitempty"`
	MaxRetainedWALBytes    int64            `json:"maxRetainedWalBytes"`
	StatementsAvailable    bool             `json:"statementsAvailable"`
	TopSlowQueries         []SlowQuery      `json:"topSlowQueries,omitempty"`
}

// ReplSlotMetric reports one pg_replication_slots entry.
type ReplSlotMetric struct {
	SlotName         string `json:"slotName"`
	SlotType         string `json:"slotType"`
	Active           bool   `json:"active"`
	RestartLSN       string `json:"restartLsn,omitempty"`
	ConfirmedFlush   string `json:"confirmedFlushLsn,omitempty"`
	RetainedWALBytes int64  `json:"retainedWalBytes"`
	FlushLagBytes    int64  `json:"flushLagBytes"`
}

// StandbyMetric reports one pg_stat_replication standby entry.
type StandbyMetric struct {
	ApplicationName string  `json:"applicationName"`
	State           string  `json:"state"`
	SyncState       string  `json:"syncState"`
	ReplayLagSec    float64 `json:"replayLagSec"`
}

// SlowQuery summarizes one top statement from pg_stat_statements.
type SlowQuery struct {
	Calls       int64   `json:"calls"`
	MeanExecMs  float64 `json:"meanExecMs"`
	MaxExecMs   float64 `json:"maxExecMs"`
	TotalExecMs float64 `json:"totalExecMs"`
	Query       string  `json:"query"`
}

// HostMetrics captures disk headroom and process file descriptors on the collector host.
type HostMetrics struct {
	DiskTotalBytes uint64  `json:"diskTotalBytes"`
	DiskFreeBytes  uint64  `json:"diskFreeBytes"`
	DiskFreePct    float64 `json:"diskFreePct"`
	OpenFDs        int     `json:"openFds"`
	ProcessRSSKB   int64   `json:"processRssKb"`
}

// Collector samples Ruxbase HTTP ops endpoints, PostgreSQL statistics, and host metrics.
type Collector struct {
	httpClient   *http.Client
	baseURL      string
	superToken   string
	pool         *pgxpool.Pool
	dbName       string
	evidencePath string
	startTime    time.Time
}

// NewCollector opens a bounded monitoring pool to PostgreSQL (preferring ruxbase_monitor).
func NewCollector(ctx context.Context, httpClient *http.Client, baseURL, superToken, monitorDSN, evidencePath string, startTime time.Time) (*Collector, error) {
	c := &Collector{
		httpClient:   httpClient,
		baseURL:      strings.TrimRight(baseURL, "/"),
		superToken:   superToken,
		evidencePath: evidencePath,
		startTime:    startTime,
	}
	if monitorDSN != "" {
		cfg, err := pgxpool.ParseConfig(monitorDSN)
		if err != nil {
			return nil, fmt.Errorf("parse monitor DSN: %w", err)
		}
		cfg.MaxConns = 3
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			return nil, fmt.Errorf("open monitor pool: %w", err)
		}
		var dbName string
		if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&dbName); err != nil {
			pool.Close()
			return nil, fmt.Errorf("query current_database: %w", err)
		}
		c.pool = pool
		c.dbName = dbName
	}
	return c, nil
}

// SetSuperuserToken refreshes the superuser token used to scrape /api/ops/*.
func (c *Collector) SetSuperuserToken(token string) {
	c.superToken = token
}

// Close closes the monitoring PostgreSQL pool.
func (c *Collector) Close() {
	if c.pool != nil {
		c.pool.Close()
	}
}

// Collect captures a single point-in-time Sample.
func (c *Collector) Collect(ctx context.Context) Sample {
	now := time.Now().UTC()
	s := Sample{
		TimestampUTC: now.Format(time.RFC3339),
		ElapsedSec:   now.Sub(c.startTime).Seconds(),
		Host:         collectHostMetrics(c.evidencePath),
	}

	// 1. Readiness probe
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/health?ready", nil)
	if err == nil {
		resp, doErr := c.httpClient.Do(req)
		if doErr != nil {
			s.CollectorError = fmt.Sprintf("health probe: %v", doErr)
		} else {
			s.HealthReadyStatus = resp.StatusCode
			s.HealthReadyOK = resp.StatusCode == http.StatusOK
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	}

	// 2. Ruxbase /api/ops/metrics & /api/ops/alerts
	if c.superToken != "" {
		if snap, err := c.fetchOpsMetrics(ctx); err == nil {
			s.RuxbaseOps = snap
		} else if s.CollectorError == "" {
			s.CollectorError = fmt.Sprintf("ops/metrics: %v", err)
		}
		if alerts, err := c.fetchOpsAlerts(ctx); err == nil {
			s.RuxbaseAlertsCount = len(alerts)
			s.RuxbaseAlerts = alerts
		}
	}

	// 3. PostgreSQL telemetry
	if c.pool != nil {
		pg, err := c.collectPostgres(ctx)
		if err != nil {
			if s.CollectorError != "" {
				s.CollectorError += "; "
			}
			s.CollectorError += fmt.Sprintf("postgres: %v", err)
		} else {
			s.Postgres = pg
		}
	}

	return s
}

func (c *Collector) fetchOpsMetrics(ctx context.Context) (*RuxbaseOpsSnapshot, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/ops/metrics", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.superToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var snap RuxbaseOpsSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (c *Collector) fetchOpsAlerts(ctx context.Context) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/ops/alerts", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", c.superToken)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var body struct {
		Alerts []map[string]any `json:"alerts"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	return body.Alerts, nil
}

func (c *Collector) collectPostgres(ctx context.Context) (*PostgresMetrics, error) {
	pg := &PostgresMetrics{
		ReplicationSlotsStatus: StatusNotConfigured,
		StandbyStatus:          StatusNotConfigured,
	}
	if err := c.pool.QueryRow(ctx, "SHOW server_version").Scan(&pg.ServerVersion); err != nil {
		return nil, err
	}
	_ = c.pool.QueryRow(ctx, "SELECT pg_database_size(current_database())").Scan(&pg.DatabaseSizeBytes)
	_ = c.pool.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&pg.WALLSN)
	_ = c.pool.QueryRow(ctx, "SELECT age(datfrozenxid)::bigint FROM pg_database WHERE datname = $1", c.dbName).Scan(&pg.MaxTxidAge)

	_ = c.pool.QueryRow(ctx, `
		SELECT xact_commit, xact_rollback, blks_read, blks_hit, deadlocks, temp_files, temp_bytes
		FROM pg_stat_database
		WHERE datname = $1
	`, c.dbName).Scan(&pg.XactCommit, &pg.XactRollback, &pg.BlksRead, &pg.BlksHit, &pg.Deadlocks, &pg.TempFiles, &pg.TempBytes)
	if pg.BlksHit+pg.BlksRead > 0 {
		pg.CacheHitRatioPct = float64(pg.BlksHit) * 100.0 / float64(pg.BlksHit+pg.BlksRead)
	} else {
		pg.CacheHitRatioPct = 100.0
	}

	_ = c.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE state = 'active'),
			count(*) FILTER (WHERE state = 'idle'),
			count(*) FILTER (WHERE state LIKE 'idle in transaction%'),
			COALESCE(max(EXTRACT(EPOCH FROM (now() - xact_start))) FILTER (WHERE xact_start IS NOT NULL), 0)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()
	`, c.dbName).Scan(&pg.ActiveConns, &pg.IdleConns, &pg.IdleInTxConns, &pg.LongestTxSec)

	_ = c.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_locks l
		JOIN pg_stat_activity a ON a.pid = l.pid
		WHERE a.datname = $1 AND NOT l.granted
	`, c.dbName).Scan(&pg.WaitingLocks)

	_ = c.pool.QueryRow(ctx, `
		SELECT COALESCE(sum(seq_scan), 0), COALESCE(sum(idx_scan), 0),
		       COALESCE(sum(n_live_tup), 0), COALESCE(sum(n_dead_tup), 0),
		       COALESCE(sum(autovacuum_count), 0)
		FROM pg_stat_user_tables
	`).Scan(&pg.SeqScans, &pg.IdxScans, &pg.LiveTuples, &pg.DeadTuples, &pg.AutovacuumCount)

	// PG 17+ moved checkpoint counters from pg_stat_bgwriter to pg_stat_checkpointer.
	if err := c.pool.QueryRow(ctx, `
		SELECT num_timed, num_requested, write_time
		FROM pg_stat_checkpointer
	`).Scan(&pg.CheckpointsTimed, &pg.CheckpointsRequested, &pg.CheckpointWriteTimeMs); err != nil {
		_ = c.pool.QueryRow(ctx, `
			SELECT checkpoints_timed, checkpoints_req, checkpoint_write_time
			FROM pg_stat_bgwriter
		`).Scan(&pg.CheckpointsTimed, &pg.CheckpointsRequested, &pg.CheckpointWriteTimeMs)
	}

	_ = c.pool.QueryRow(ctx, "SELECT wal_records, wal_bytes::bigint FROM pg_stat_wal").Scan(&pg.WALRecords, &pg.WALBytes)
	_ = c.pool.QueryRow(ctx, "SELECT COALESCE(failed_count, 0) FROM pg_stat_archiver").Scan(&pg.WALArchiveFailedCount)

	// Replication slots: mark NOT_CONFIGURED if none exist (never report fake 0 lag).
	slotRows, err := c.pool.Query(ctx, `
		SELECT slot_name, slot_type, active,
		       COALESCE(restart_lsn::text, ''),
		       COALESCE(confirmed_flush_lsn::text, ''),
		       COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), restart_lsn)::bigint, 0),
		       COALESCE(pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)::bigint, 0)
		FROM pg_replication_slots
		WHERE database = $1 OR database IS NULL
	`, c.dbName)
	if err == nil {
		defer slotRows.Close()
		for slotRows.Next() {
			var sm ReplSlotMetric
			if slotRows.Scan(&sm.SlotName, &sm.SlotType, &sm.Active, &sm.RestartLSN, &sm.ConfirmedFlush, &sm.RetainedWALBytes, &sm.FlushLagBytes) == nil {
				pg.ReplicationSlots = append(pg.ReplicationSlots, sm)
				if sm.RetainedWALBytes > pg.MaxRetainedWALBytes {
					pg.MaxRetainedWALBytes = sm.RetainedWALBytes
				}
			}
		}
		if len(pg.ReplicationSlots) > 0 {
			pg.ReplicationSlotsStatus = "CONFIGURED"
		}
	}

	// Physical/logical standbys in pg_stat_replication: mark NOT_CONFIGURED if none.
	repRows, err := c.pool.Query(ctx, `
		SELECT COALESCE(application_name, ''), COALESCE(state, ''), COALESCE(sync_state, ''),
		       COALESCE(EXTRACT(EPOCH FROM replay_lag), 0)
		FROM pg_stat_replication
	`)
	if err == nil {
		defer repRows.Close()
		for repRows.Next() {
			var st StandbyMetric
			if repRows.Scan(&st.ApplicationName, &st.State, &st.SyncState, &st.ReplayLagSec) == nil {
				pg.StandbyReplicas = append(pg.StandbyReplicas, st)
			}
		}
		if len(pg.StandbyReplicas) > 0 {
			pg.StandbyStatus = "CONFIGURED"
		}
	}

	// pg_stat_statements top slow queries if installed
	stmtRows, err := c.pool.Query(ctx, `
		SELECT calls, mean_exec_time, max_exec_time, total_exec_time,
		       left(regexp_replace(query, E'[\\n\\r\\t]+', ' ', 'g'), 180)
		FROM pg_stat_statements s
		JOIN pg_database d ON d.oid = s.dbid
		WHERE d.datname = $1
		ORDER BY total_exec_time DESC
		LIMIT 5
	`, c.dbName)
	if err == nil {
		pg.StatementsAvailable = true
		defer stmtRows.Close()
		for stmtRows.Next() {
			var sq SlowQuery
			if stmtRows.Scan(&sq.Calls, &sq.MeanExecMs, &sq.MaxExecMs, &sq.TotalExecMs, &sq.Query) == nil {
				pg.TopSlowQueries = append(pg.TopSlowQueries, sq)
			}
		}
	}

	return pg, nil
}

func collectHostMetrics(path string) HostMetrics {
	if path == "" {
		path = "/"
	}
	var hm HostMetrics
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err == nil && st.Blocks > 0 {
		bsize := uint64(st.Bsize)
		hm.DiskTotalBytes = st.Blocks * bsize
		hm.DiskFreeBytes = st.Bavail * bsize
		hm.DiskFreePct = float64(st.Bavail) * 100.0 / float64(st.Blocks)
	}
	if entries, err := os.ReadDir("/proc/self/fd"); err == nil {
		hm.OpenFDs = len(entries)
	}
	if raw, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				var kb int64
				_, _ = fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(line, "VmRSS:")), "%d", &kb)
				hm.ProcessRSSKB = kb
				break
			}
		}
	}
	return hm
}
