package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OpsSnap mirrors GET /api/ops/metrics JSON response.
type OpsSnap struct {
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

// PGStatSnapshot holds raw PostgreSQL cumulative counters at a point in time.
type PGStatSnapshot struct {
	CapturedAt      time.Time
	ServerVersion   string
	XactCommit      int64
	XactRollback    int64
	BlksRead        int64
	BlksHit         int64
	Deadlocks       int64
	TempBytes       int64
	TupInserted     int64
	TupUpdated      int64
	TupDeleted      int64
	TupFetched      int64
	WALLSN          string
	ActiveConns     int
	IdleConns       int
	IdleInTxConns   int
	WaitingLocks    int
	WaitEvents      map[string]int
	ReplSlotLagByte int64
}

// PGDeltaStats summarizes PostgreSQL activity across a measurement window.
type PGDeltaStats struct {
	ServerVersion   string         `json:"serverVersion"`
	DurationSec     float64        `json:"durationSeconds"`
	TPS             float64        `json:"tps"`
	XactCommit      int64          `json:"xactCommit"`
	XactRollback    int64          `json:"xactRollback"`
	Deadlocks       int64          `json:"deadlocks"`
	CacheHitRatio   float64        `json:"cacheHitRatioPct"`
	BlksRead        int64          `json:"blksRead"`
	BlksHit         int64          `json:"blksHit"`
	TempBytes       int64          `json:"tempBytes"`
	WALBytes        int64          `json:"walBytes"`
	WALMBPerSec     float64        `json:"walMBPerSec"`
	ActiveConnsEnd  int            `json:"activeConnsEnd"`
	IdleConnsEnd    int            `json:"idleConnsEnd"`
	IdleInTxEnd     int            `json:"idleInTxEnd"`
	WaitingLocksEnd int            `json:"waitingLocksEnd"`
	WaitEventsEnd   map[string]int `json:"waitEventsEnd,omitempty"`
	ReplSlotLagByte int64          `json:"replSlotLagBytes,omitempty"`
}

// PGTopQuery is one row from pg_stat_statements.
type PGTopQuery struct {
	Calls         int64   `json:"calls"`
	TotalExecMs   float64 `json:"totalExecMs"`
	MeanExecMs    float64 `json:"meanExecMs"`
	MaxExecMs     float64 `json:"maxExecMs"`
	Rows          int64   `json:"rows"`
	SharedBlksHit int64   `json:"sharedBlksHit"`
	SharedBlksRd  int64   `json:"sharedBlksRead"`
	Query         string  `json:"query"`
}

type pgTelemetry struct {
	pool   *pgxpool.Pool
	dbName string
}

func openPGTelemetry(ctx context.Context, dsn string) (*pgTelemetry, error) {
	if dsn == "" {
		return nil, nil
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, err
	}
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var dbName string
	if err := pool.QueryRow(ctx, "SELECT current_database()").Scan(&dbName); err != nil {
		pool.Close()
		return nil, err
	}
	return &pgTelemetry{pool: pool, dbName: dbName}, nil
}

func (p *pgTelemetry) Close() {
	if p != nil && p.pool != nil {
		p.pool.Close()
	}
}

func (p *pgTelemetry) verifyAppRoleRestricted(ctx context.Context, roleName string) error {
	if p == nil {
		return nil
	}
	var super, bypass bool
	err := p.pool.QueryRow(ctx, "SELECT rolsuper, rolbypassrls FROM pg_roles WHERE rolname = $1", roleName).Scan(&super, &bypass)
	if err != nil {
		return fmt.Errorf("verify role %q: %w", roleName, err)
	}
	if super || bypass {
		return fmt.Errorf("SECURITY VIOLATION: role %q has rolsuper=%v rolbypassrls=%v", roleName, super, bypass)
	}
	return nil
}

func (p *pgTelemetry) resetStatements(ctx context.Context) {
	if p == nil {
		return
	}
	_, _ = p.pool.Exec(ctx, "SELECT pg_stat_statements_reset()")
}

func (p *pgTelemetry) snapshot(ctx context.Context) (*PGStatSnapshot, error) {
	if p == nil {
		return nil, nil
	}
	s := &PGStatSnapshot{
		CapturedAt: time.Now(),
		WaitEvents: map[string]int{},
	}
	_ = p.pool.QueryRow(ctx, "SHOW server_version").Scan(&s.ServerVersion)
	_ = p.pool.QueryRow(ctx, "SELECT pg_current_wal_lsn()::text").Scan(&s.WALLSN)
	err := p.pool.QueryRow(ctx, `
		SELECT xact_commit, xact_rollback, blks_read, blks_hit, deadlocks, temp_bytes,
		       tup_inserted, tup_updated, tup_deleted, tup_fetched
		FROM pg_stat_database
		WHERE datname = $1
	`, p.dbName).Scan(
		&s.XactCommit, &s.XactRollback, &s.BlksRead, &s.BlksHit, &s.Deadlocks, &s.TempBytes,
		&s.TupInserted, &s.TupUpdated, &s.TupDeleted, &s.TupFetched,
	)
	if err != nil {
		return nil, err
	}

	rows, err := p.pool.Query(ctx, `
		SELECT COALESCE(state, 'unknown'), COALESCE(wait_event_type || ':' || wait_event, 'none'), count(*)
		FROM pg_stat_activity
		WHERE datname = $1 AND pid <> pg_backend_pid()
		GROUP BY 1, 2
	`, p.dbName)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var state, we string
			var cnt int
			if rows.Scan(&state, &we, &cnt) == nil {
				switch state {
				case "active":
					s.ActiveConns += cnt
				case "idle":
					s.IdleConns += cnt
				case "idle in transaction", "idle in transaction (aborted)":
					s.IdleInTxConns += cnt
				}
				if we != "none" && state == "active" {
					s.WaitEvents[we] += cnt
				}
			}
		}
	}

	_ = p.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM pg_locks l
		JOIN pg_stat_activity a ON a.pid = l.pid
		WHERE a.datname = $1 AND NOT l.granted
	`, p.dbName).Scan(&s.WaitingLocks)

	_ = p.pool.QueryRow(ctx, `
		SELECT COALESCE(max(pg_wal_lsn_diff(pg_current_wal_lsn(), confirmed_flush_lsn)), 0)::bigint
		FROM pg_replication_slots
		WHERE database = $1
	`, p.dbName).Scan(&s.ReplSlotLagByte)

	return s, nil
}

func (p *pgTelemetry) diff(ctx context.Context, before, after *PGStatSnapshot) *PGDeltaStats {
	if p == nil || before == nil || after == nil {
		return nil
	}
	dur := after.CapturedAt.Sub(before.CapturedAt).Seconds()
	if dur <= 0 {
		dur = 0.001
	}
	commits := after.XactCommit - before.XactCommit
	rollbacks := after.XactRollback - before.XactRollback
	blksHit := after.BlksHit - before.BlksHit
	blksRead := after.BlksRead - before.BlksRead
	hitRatio := 100.0
	if blksHit+blksRead > 0 {
		hitRatio = round3(float64(blksHit) * 100.0 / float64(blksHit+blksRead))
	}

	var walBytes int64
	if before.WALLSN != "" && after.WALLSN != "" {
		_ = p.pool.QueryRow(ctx, "SELECT pg_wal_lsn_diff($1::pg_lsn, $2::pg_lsn)::bigint", after.WALLSN, before.WALLSN).Scan(&walBytes)
	}

	return &PGDeltaStats{
		ServerVersion:   after.ServerVersion,
		DurationSec:     round3(dur),
		TPS:             round2(float64(commits+rollbacks) / dur),
		XactCommit:      commits,
		XactRollback:    rollbacks,
		Deadlocks:       after.Deadlocks - before.Deadlocks,
		CacheHitRatio:   hitRatio,
		BlksRead:        blksRead,
		BlksHit:         blksHit,
		TempBytes:       after.TempBytes - before.TempBytes,
		WALBytes:        walBytes,
		WALMBPerSec:     round3(float64(walBytes) / (1024 * 1024) / dur),
		ActiveConnsEnd:  after.ActiveConns,
		IdleConnsEnd:    after.IdleConns,
		IdleInTxEnd:     after.IdleInTxConns,
		WaitingLocksEnd: after.WaitingLocks,
		WaitEventsEnd:   after.WaitEvents,
		ReplSlotLagByte: after.ReplSlotLagByte,
	}
}

func (p *pgTelemetry) topStatements(ctx context.Context, limit int) []PGTopQuery {
	if p == nil {
		return nil
	}
	rows, err := p.pool.Query(ctx, `
		SELECT calls, total_exec_time, mean_exec_time, max_exec_time, rows,
		       shared_blks_hit, shared_blks_read, left(regexp_replace(query, E'[\\n\\r\\t]+', ' ', 'g'), 240)
		FROM pg_stat_statements s
		JOIN pg_database d ON d.oid = s.dbid
		WHERE d.datname = $1
		ORDER BY total_exec_time DESC
		LIMIT $2
	`, p.dbName, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []PGTopQuery{}
	for rows.Next() {
		var q PGTopQuery
		if rows.Scan(&q.Calls, &q.TotalExecMs, &q.MeanExecMs, &q.MaxExecMs, &q.Rows, &q.SharedBlksHit, &q.SharedBlksRd, &q.Query) == nil {
			q.TotalExecMs = round3(q.TotalExecMs)
			q.MeanExecMs = round3(q.MeanExecMs)
			q.MaxExecMs = round3(q.MaxExecMs)
			out = append(out, q)
		}
	}
	return out
}

func fetchOpsMetrics(client *http.Client, baseURL, superToken string) *OpsSnap {
	if superToken == "" || baseURL == "" {
		return nil
	}
	status, raw, err := doJSON(client, http.MethodGet, baseURL+"/api/ops/metrics", superToken, nil)
	if err != nil || status != http.StatusOK {
		return nil
	}
	var snap OpsSnap
	if json.Unmarshal(raw, &snap) != nil {
		return nil
	}
	return &snap
}

func fetchOpsJSON(client *http.Client, baseURL, path, superToken string) map[string]any {
	status, raw, err := doJSON(client, http.MethodGet, baseURL+path, superToken, nil)
	if err != nil {
		return map[string]any{"error": err.Error(), "status": status}
	}
	out := map[string]any{"status": status}
	var parsed any
	if json.Unmarshal(raw, &parsed) == nil {
		out["body"] = parsed
	} else {
		out["raw"] = string(raw)
	}
	return out
}
