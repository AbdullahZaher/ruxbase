# Ruxbase Enterprise Stress Testing & Scalability Certification Harness

Reproducible, isolated, high-intensity stress testing harness covering Tests A–H,
mandatory post-test data integrity verification, Go `pprof` profiling, PostgreSQL
`pg_stat_statements` / `EXPLAIN (ANALYZE, BUFFERS)` inspection, and PG17/18 parity.

## Safety & Isolation Rules

- Always runs in a dedicated, disposable Docker Compose project (`ruxbase-stress`) with isolated networks and volumes.
- Never targets production, staging, or shared developer databases.
- Ruxbase application instances ALWAYS connect using the least-privilege non-superuser role `ruxbase_app` (`NOSUPERUSER`, `NOBYPASSRLS`) with PostgreSQL Row-Level Security (`RLS`) enabled on multi-tenant fixtures.
- Bounded load escalation with emergency stop thresholds and automatic container/volume cleanup (`trap cleanup EXIT`).

## Layout

| Path | Purpose |
| --- | --- |
| `docker-compose.yml` | Isolated multi-profile topology (single-node, 1/2/3-node cluster, `ruxbase-pprof`, `netproxy`, `runner`, PG17/18) with explicit cgroup v2 CPU/RAM limits |
| `postgres.conf` | Production-like PostgreSQL 17/18 settings (`wal_level=logical`, `pg_stat_statements`, `track_io_timing`, `log_lock_waits`) |
| `init-stress-db.sql` | Provisions non-superuser `ruxbase_app`, `ruxbase_wal`, disposable databases, and extensions (`vector`, `pgcrypto`, `pg_stat_statements`) |
| `cmd/stress/` | Main HTTP/PG load generator, open-loop & closed-loop engines, realtime SSE fan-out, chaos timeline probes, endurance monitor, and integrity verifier |
| `cmd/corestress/` | Go-core stress runner for Durable Jobs, Transactional Outbox, `StorageManager` (multipart, signed URLs, large objects), and isolated Goja `FunctionManager` |
| `cmd/ruxbase-pprof/` | Loopback-only `net/http/pprof` diagnostic wrapper around unmodified `ruxbase.New()` (Section 13 profiling only) |
| `cmd/netproxy/` | Deterministic TCP latency/jitter/cut proxy for Test G chaos experiments |
| `sdk_stress.mjs` | Official PocketBase JS SDK (`pocketbase@0.28.1`) concurrent stress & tenant-isolation check |
| `scripts/run-all.sh` | End-to-end reproducible orchestrator (`STRESS_PHASES=...`) |
| `scripts/monitor.sh` | Continuous Docker cgroup v2 CPU/RAM/FD telemetry sampler |
| `scripts/analyze.py` | Statistical summary generator & SHA-256 artifact manifest builder |

## Quick Execution

Run the full campaign against the clean repository checkout:

```bash
chmod +x bench/stress/scripts/run-all.sh bench/stress/scripts/monitor.sh
STRESS_PHASES="build env test-a test-b test-d test-f test-c test-e test-g profile pg17 test-h analyze" \
  ./bench/stress/scripts/run-all.sh
```

Run a single phase (for example, PG18 Test A only or extended 60-minute Test H endurance):

```bash
STRESS_PHASES="build env test-a analyze" ./bench/stress/scripts/run-all.sh
STRESS_PHASES="build env test-h analyze" STRESS_ENDURANCE_DUR=3600s ./bench/stress/scripts/run-all.sh
```

To build the tested Ruxbase binary from a clean detached `git worktree` (preserving unrelated local edits):

```bash
STRESS_CANDIDATE_SRC=/path/to/clean-worktree ./bench/stress/scripts/run-all.sh
```

## Important Note on `W11-05` and `W11-06`

- Test H (`test-h`) is an isolated endurance test and does **not** replace the mandatory `W11-05` 168-hour continuous staging soak.
- This harness evaluates Ruxbase's own scalability limits and failure behavior; it does **not** claim superiority over Supabase Self-Hosted without matched side-by-side `W11-06` evidence.
