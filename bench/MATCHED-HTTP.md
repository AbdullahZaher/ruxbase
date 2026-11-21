# W00-08 matched HTTP read and create workload

This workload exercises a single Ruxbase instance and an official Supabase
Self-Hosted stack against separate disposable databases. Run both stacks on the
same host and run only one at a time. Bind the tested API gateway to loopback.
Both APIs handle anonymous requests. On Supabase use the disposable stack's
anon key through the gateway. On Ruxbase use collection rules permitting
anonymous list, view and create. These rules are for synthetic fixtures.

The workload uses the same fixture IDs, fields and values, an index on `n`, and
the same worker settings. Reads alternate according to a seeded per-worker
sequence between a primary-key lookup and a page of up to 20 records. The page
filters `published = true` and `n < threshold` and sorts by descending `n`.
Both APIs project the same fields and omit total counts. Creates use the same
payload and request the stored representation. The JSON envelopes, generated
IDs and API routing differ. A faster target can consume more of the same worker
sequence within a timed run.

## Prepare the fixtures

Build the load generator from the Ruxbase repository root with Go 1.27.4.

```sh
go build -trimpath -o /tmp/ruxbase-bench ./bench
```

Start a fresh Ruxbase instance with an external disposable PostgreSQL database
and provision a disposable superuser. Run setup once per fresh database.

```sh
/tmp/ruxbase-bench -scenario matched-setup -backend ruxbase \
  -url http://127.0.0.1:8090 -records 10000 \
  -email "$BENCH_RUXBASE_EMAIL" -password "$BENCH_RUXBASE_PASSWORD"
```

For the Supabase runs, start the official Compose profile at
`self-hosted/v0.8.4` (commit `189adda99d1b56da55fc5a6ee3f849354aaea92a`).
From the Ruxbase checkout, provision the fixture using the admin connection to
the fresh Supabase database. Supply its anon key via an environment variable.
Use the official stack gateway port when running the workload.

```sh
psql -X "$BENCH_SUPABASE_DATABASE_URL" -v ON_ERROR_STOP=1 \
  -v records=10000 -f bench/matched-supabase.sql
export BENCH_SUPABASE_ANON_KEY
```

Fixture creation refuses to replace an existing collection or table. A setup
failure requires a new disposable database. Keep reads before writes because
writes grow the fixture table. Provision a clean dataset for each independent
trial.

## Measure

Set `BENCH_BACKEND` to `ruxbase` or `supabase` and `BENCH_ORIGIN` to the
corresponding loopback origin. Use identical settings for both stacks and
write each trial's output to a fresh file.

```sh
/tmp/ruxbase-bench -scenario matched-reads -backend "$BENCH_BACKEND" \
  -url "$BENCH_ORIGIN" -records 10000 -c 50 -d 20s >> "$BENCH_RESULTS"
/tmp/ruxbase-bench -scenario matched-writes -backend "$BENCH_BACKEND" \
  -url "$BENCH_ORIGIN" -records 10000 -c 50 -d 20s >> "$BENCH_RESULTS"
```

Each run checks fixture records before measurement and writes one JSON line
with successful requests, errors, throughput and latency percentiles. Measured
requests with an unexpected response status cause a failing exit after the
JSON row is emitted. Benchmark resource attribution remains external to this
load generator. Use matched total CPU and memory limits and record the
hardware, database settings, image digests, enabled services, warm-up and trial
order with container-level statistics for each stack. Scope this workload to
anonymous HTTP reads and creates. Authentication, realtime and storage need
their own comparable profiles.
