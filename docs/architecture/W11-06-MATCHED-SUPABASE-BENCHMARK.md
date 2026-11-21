# W11-06 / W00-08 / JOURNEY-11: Matched Supabase Self-Hosted Benchmark Evidence & Status

**Document:** `docs/architecture/W11-06-MATCHED-SUPABASE-BENCHMARK.md`  
**Requirement IDs:** `W11-06 / P1 / GATE`, `W00-08 / P1 / CORE`, `JOURNEY-11`  
**Status:** **`PENDING_MATCHED_BENCHMARK` (Ruxbase Matched Harness Verified Live; Side-by-Side Supabase Self-Hosted Stack Pending Dedicated Host)**  
**Candidate Commit Baseline:** `6b443e4b2aaa9ee50276416af5830ba9daf11db6` (`release/final-qualification-20261122`)  
**Date:** `2026-11-22`  

---

## 1. Benchmark Scope & Non-Fabrication Policy

Per `MASTER-ROADMAP.md`, `MASTER-CHECKLIST.md`, and `docs/BENCHMARKS.md`:
- Compare **only** against **Supabase Self-Hosted** (pinned target: official Docker Compose profile `self-hosted/v0.8.4`, commit `189adda99d1b56da55fc5a6ee3f849354aaea92a`), **never** Supabase Cloud.
- Do **not** infer Supabase Self-Hosted superiority from existing PocketBase v0.40.6 comparisons (`docs/BENCHMARKS.md`).
- Do **not** fabricate or extrapolate Supabase Self-Hosted numbers when the full multi-container Supabase Self-Hosted stack (`Postgres + Kong + PostgREST + GoTrue + Realtime + Storage + Meta/Studio`) has not been executed under matched cgroup limits on an isolated comparison host.

---

## 2. Verified Matched Benchmark Harness & Live Ruxbase Execution Evidence

The matched HTTP workload harness (`bench/matched.go`, `bench/matched-supabase.sql`, `bench/matched_test.go`, `bench/MATCHED-HTTP.md`) was implemented in [PR #40](https://github.com/AbdullahZaher/ruxbase/pull/40) (`046d34b4`) and verified during Final Release Qualification on `2026-11-22`:

### 2.1 Unit & Contract Verification
- Command: `go test -count=1 ./bench/...`
- Result: **PASS (`2.531s`)** — verifies loopback origin guards, identical field projection (`id,title,body,n,published`), identical seeded PRNG worker sequences (`rand.NewPCG(8, uint64(worker))`), filter equivalence (`n < threshold && published = true`, sorted by `-n`, `limit=20`), and redirect credential protection for both `ruxbase` and `supabase` backends.

### 2.2 Live Ruxbase Execution of Matched Workload (`2026-11-22`)
Executed against a clean disposable PostgreSQL 18 database (`pgvector/pgvector:0.8.7-pg18-trixie`, server version `180007`, non-superuser runtime role `ruxbase_app`, `--dev=false`) on the local host (`darwin/arm64`, Go `1.27.0`):

```json
{"target":"ruxbase","scenario":"matched-reads","concurrency":16,"durationSeconds":3.001473167,"requests":20835,"errors":0,"rps":6941.591292260252,"p50ms":2.067,"p95ms":4.2,"p99ms":5.776}
{"target":"ruxbase","scenario":"matched-writes","concurrency":16,"durationSeconds":3.003164667,"requests":13134,"errors":0,"rps":4373.386562622341,"p50ms":3.463,"p95ms":5.61,"p99ms":7.015}
```

| Target | Scenario | Concurrency | Requests | Errors | Throughput (req/s) | p50 (ms) | p95 (ms) | p99 (ms) |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| `ruxbase` (1 node + PG18, `ruxbase_app`) | `matched-reads` (50% PK view, 50% filtered sorted page) | 16 | 20,835 | 0 | **6,941.59** | 2.067 | 4.200 | 5.776 |
| `ruxbase` (1 node + PG18, `ruxbase_app`) | `matched-writes` (5-field record create + representation) | 16 | 13,134 | 0 | **4,373.39** | 3.463 | 5.610 | 7.015 |
| `supabase` (`self-hosted/v0.8.4`) | `matched-reads` / `matched-writes` | — | — | — | **`PENDING_MATCHED_BENCHMARK`** | — | — | — |

---

## 3. Procedure to Complete `W11-06` / `W00-08` / `JOURNEY-11` on a Dedicated Comparison Host

To transition `W11-06`, `W00-08`, and `JOURNEY-11` to `[x]` before final Production `GO`:

1. **Provision a Dedicated Linux Benchmark Host** with Docker cgroup v2 CPU/memory attribution enabled (`cgroupDriver=systemd` or `cgroupfs`, not `none`).
2. **Execute Ruxbase Trials (1-node and 3-node HA)** using `bench/MATCHED-HTTP.md` (`10,000` seeded records, concurrency `50` and `200`, `20s` duration x `3` trials) with fixed total CPU and memory limits across all Ruxbase + PostgreSQL containers.
3. **Stop Ruxbase Containers & Start Official Supabase Self-Hosted (`self-hosted/v0.8.4`, commit `189adda99d1b56da55fc5a6ee3f849354aaea92a`)** under the exact same total CPU and memory budget across all mandatory Supabase containers (`db`, `kong`, `rest`, `auth`, `realtime`, `storage`, `meta`).
4. **Seed Identical Fixture via `bench/matched-supabase.sql`** and execute `/tmp/ruxbase-bench -scenario matched-reads -backend supabase` and `matched-writes -backend supabase` with identical worker concurrency and duration.
5. **Commit Raw `.jsonl`, `.resources.jsonl`, and `.environment.txt` Artifacts** to `bench/results/` and update `docs/BENCHMARKS.md` and `MASTER-CHECKLIST.md`.
