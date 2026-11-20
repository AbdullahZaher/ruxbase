# Benchmarks

These benchmarks compare ruxbase (a single instance and 3 instances) with
PocketBase v0.40.6 on the same machine. They show the order of magnitude of the
throughput and latencies; run them on your own hardware before drawing
conclusions for a production setup.

Run them with:

```sh
./bench/run.sh
```

The script builds ruxbase from the sources and PocketBase v0.40.6 from the
official upstream tag in an isolated checkout (verified against the pinned commit), starts the environment of `bench/docker-compose.yml` and runs the
load generator (`bench/main.go`), which works with any PocketBase compatible
server.

## Reproducible Ruxbase smoke baseline

The benchmark can run independently of the imported Git ancestry:

```sh
BENCH_TARGETS=ruxbase BENCH_DURATION=8s BENCH_CONCURRENCY=8 \
  BENCH_RUNS=1 BENCH_RECORDS=1000 BENCH_CLIENTS=20 \
  BENCH_EVENTS=20 BENCH_RATE=5 bash bench/run.sh
```

Run only against the disposable benchmark Compose project; its setup and
cleanup delete benchmark containers and volumes. Do not reuse production data.
The `benchmark baseline` workflow executes this bounded workload, checks fresh
JSON output, zero request errors, delivery counts and resource samples, and
uploads the environment and measurement artifacts.

The companion `*.resources.jsonl` records one Docker CPU/memory sample per
container at idle and during each workload. PostgreSQL is included when
measuring Ruxbase. These are Docker-reported samples, not process RSS or peak
memory measurements. Short runs are a functional measurement smoke test,
not a statistically robust benchmark or a Supabase comparison. The existing
environment uses public collection rules and disposable database credentials;
production-equivalent authorization and matched CPU/RAM budgets remain
requirements for competitive performance evaluation.

## CI smoke evidence — 2026-11-20

[Run #44665505623](https://github.com/AbdullahZaher/ruxbase/actions/runs/44665505623)
passed on candidate `98403be2c933192fb4d9f3c5df01d29753f25e61`.
This is one single-instance disposable run: 1,000 initial records, 8 workers,
8 seconds per CRUD scenario, 20 SSE clients and 20 events. All request and
delivery checks passed.

| Scenario | Successful requests / deliveries | Requests/s | P95 (ms) | P99 (ms) |
| --- | ---: | ---: | ---: | ---: |
| Reads | 28,443 | 3,554.50 | 5.066 | 7.292 |
| Writes | 33,900 | 4,236.29 | 3.485 | 4.707 |
| Realtime | 400 deliveries | 99.85 deliveries/s | 2.295 | 3.159 |

| Sample | Ruxbase MiB | PostgreSQL MiB | Total MiB | Combined CPU % |
| --- | ---: | ---: | ---: | ---: |
| Idle | 18.45 | 145.00 | 163.45 | 1.87 |
| Reads | 18.84 | 150.30 | 169.14 | 218.48 |
| Writes | 18.66 | 170.30 | 188.96 | 254.58 |
| Realtime phase | 17.82 | 194.90 | 212.72 | 1.17 |

These are point-in-time Docker stats (100% CPU = one logical CPU), not peaks,
RSS, or average load. The realtime sample was near idle and is not evidence
of sustained realtime CPU cost. No Supabase target was executed. Public
rules, short duration, sequential workloads, and default test credentials
limit applicability. The separate full CI on that candidate failed in
`TestNotifyWatcher_SettingsUpdate`; successful benchmark evidence does not
certify the candidate. Full CI must pass on any replacement candidate.

## Guarded connection pool microbenchmarks (W01-M01)

Ruxbase incorporates a guarded connection pool (`internal/pgrolecheck/connector.go`) to verify connection-level role isolation and `search_path` constraints at checkout time. Serial microbenchmarks (three trials, 200 ordinary and 200 guarded samples per trial) evaluate the checkout latency overhead:

| PostgreSQL Version | Ordinary Pool Checkout (p95) | Guarded Pool Checkout (p95) | Overhead Factor |
|---|---|---|---|
| PostgreSQL 17 | 0.205 ms | 1.525 ms | ~7.4x |
| PostgreSQL 18 | 0.093 ms | 0.769 ms | ~8.2x |

### Proposed performance budget (M01 Scope A)

- **Target:** Guarded connection checkout p95 < 2.0 ms across supported PostgreSQL versions (17 and 18).
- **Scope:** Bounded to Scope A (restricted prepared runtime profile). General production serving under Scope B will require dedicated connection pooling optimization (e.g. prepared statement reuse, pipelined verification).

## Environment

- Intel Core Ultra 9 275HX, Docker Desktop 29.7 (24 CPUs and 16 GB available to
  Docker), Windows 11.
- All services run in the same Docker network with CPU limits, and each target
  runs alone (the services of the other targets are stopped):

  | Target            | Services                                                                                       |
  | ----------------- | ---------------------------------------------------------------------------------------------- |
  | `pocketbase`      | PocketBase v0.40.6 (SQLite): 2 CPUs                                                            |
  | `ruxbase`         | 1 ruxbase instance (2 CPUs) + Postgres 18 (4 CPUs)                                             |
  | `ruxbase-3-nodes` | 3 ruxbase instances (2 CPUs each) + Postgres 18 (4 CPUs), with the realtime replication source |

- The load generator runs in its own container (4 CPUs). For the 3 instances it
  spreads the requests over them (client side load balancing).
- Postgres 18.6 with `shared_buffers=1GB`. The binaries are built with Go 1.27.

Note that ruxbase uses more resources than PocketBase in these setups (the
Postgres server), and that a Postgres server on another machine adds a network
round trip to each query.

## Scenarios

- **Reads:** 50% views of a random record and 50% list pages (`perPage=20`, a
  filter on an indexed number field, sort and `skipTotal`), over 10 000 records.
  Public rules.
- **Writes:** concurrent record creates (5 fields). Public rules.
- **Realtime:** 500 clients subscribed to the collection, 200 records created at
  20 per second; the latency is measured from the create request to the event
  received by each client (100 000 deliveries).

Each reads and writes measurement lasts 20 seconds and is repeated 3 times (the
table shows the median run).

## Results

Measured on 2026-10-26 (`bench/results/20261026-074039.jsonl`).

### Reads

| Target          | Concurrency | Requests/s | p50 (ms) | p95 (ms) | p99 (ms) |
| --------------- | ----------: | ---------: | -------: | -------: | -------: |
| pocketbase      |          50 |      7 324 |      3.6 |     24.2 |     41.9 |
| pocketbase      |         200 |      6 192 |     17.8 |     87.5 |    360.8 |
| ruxbase         |          50 |      8 766 |      4.4 |     14.6 |     23.5 |
| ruxbase         |         200 |      8 166 |     16.0 |     74.8 |    126.3 |
| ruxbase-3-nodes |          50 |     19 657 |      2.0 |      6.7 |     10.7 |
| ruxbase-3-nodes |         200 |     19 728 |      7.3 |     27.7 |     42.8 |

### Writes

| Target          | Concurrency | Requests/s | p50 (ms) | p95 (ms) | p99 (ms) |
| --------------- | ----------: | ---------: | -------: | -------: | -------: |
| pocketbase      |          50 |      3 077 |      7.8 |     61.3 |    136.5 |
| pocketbase      |         200 |      3 079 |     35.9 |    198.5 |    337.8 |
| ruxbase         |          50 |      8 161 |      4.6 |     14.1 |     26.1 |
| ruxbase         |         200 |      5 953 |     16.5 |     72.6 |    202.0 |
| ruxbase-3-nodes |          50 |      9 952 |      2.7 |     16.1 |     45.4 |
| ruxbase-3-nodes |         200 |     13 341 |      8.5 |     47.4 |    117.3 |

### Realtime fan-out

| Target          | Clients | Deliveries | Missing | p50 (ms) | p95 (ms) | p99 (ms) |
| --------------- | ------: | ---------: | ------: | -------: | -------: | -------: |
| pocketbase      |     500 |    100 000 |       0 |      4.2 |      9.5 |     11.5 |
| ruxbase         |     500 |    100 000 |       0 |      5.5 |     13.8 |     33.6 |
| ruxbase-3-nodes |     500 |    100 000 |       0 |     11.1 |     28.0 |     32.0 |

There were no errors in any scenario.

## Observations

- **Writes** are where Postgres makes the biggest difference: SQLite has a
  single writer, so the concurrent creates of PocketBase queue up (p95 of 61-199
  ms), while ruxbase keeps a p95 of 14 ms with 50 concurrent writers.
- **Reads** of a single ruxbase instance are slightly faster than PocketBase in
  this setup, and 3 instances more than double the throughput (the Postgres
  server, not the instances, becomes the limit).
- **Realtime:** with a single instance the events come from the app hooks, as in
  PocketBase. With 3 instances they come from the Postgres logical replication
  (decoded by the leader and published to the other instances through
  `LISTEN/NOTIFY`), which adds a few milliseconds. The replication source is
  designed for the changes made outside of the API too.
- **Replication catch up:** during the sustained writes of the 3 instances (more
  than 10 000 creates per second for a minute) the replication slot falls
  behind; it caught up 20 seconds after the writes stopped. The realtime events
  of a sustained write peak of that size are delivered late, not lost.

The benchmarks found and fixed three performance issues of ruxbase before these
results: the database connections were reopened under a moderate load (the idle
pool was smaller than the open one), the replication leader stored and published
each transaction separately, and each instance loaded each replicated record
with a separate query.

## Limitations

- A single machine with Docker Desktop: the containers share the host CPU and
  memory, and the network is a local bridge.
- The load generator balances the requests over the 3 instances itself; a load
  balancer in front of them (eg. nginx) adds its own overhead, which is not
  measured here.
- The scenarios use public rules; the API rules add their filters to the queries
  (and the realtime checks) in both PocketBase and ruxbase.
- Supabase is not included.
