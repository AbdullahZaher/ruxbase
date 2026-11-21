# Wave 06 — Durable Jobs & Event Architecture: Separation of Concerns

## 1. Executive Summary

This specification establishes the architectural contract for **separating transient scheduling/notifications from committed durable background jobs and transactional outbox events** in Ruxbase (Milestone W06-01).

Prior to Wave 06, Ruxbase relied on two primary asynchronous mechanisms:
1. **In-Memory Cron (`tools/cron`)**: Ephemeral, memory-only timer loops. If the node restarts, in-flight schedules are cancelled and scheduled ticks during downtime are missed.
2. **PostgreSQL NOTIFY / Replication Streams (`core/cluster`)**: Ephemeral cluster notifications. Messages carry transient identifiers; delivery is best-effort and not durable across process crashes.

Wave 06 introduces a **PostgreSQL-native durable jobs engine and transactional outbox**, guaranteeing that committed business workflows are never lost, survive process terminations and database restarts, and execute with strictly bounded at-least-once delivery and idempotency controls.

---

## 2. Classification Matrix: Transient vs. Durable

| Dimension | Transient Cron (`tools/cron`) | Transient Cluster (`NOTIFY`) | Durable Jobs (`_jobs`) | Transactional Outbox (`_outbox_events`) |
|---|---|---|---|---|
| **Storage Engine** | In-memory goroutine | PostgreSQL pub/sub | PostgreSQL `_jobs` table | PostgreSQL `_outbox_events` table |
| **Transaction Boundary** | Non-transactional | Post-commit notification | Atomic with caller transaction | Atomic with caller transaction |
| **Crash Survivability** | Lost on process restart | Lost if node is down | 100% durable in PostgreSQL | 100% durable in PostgreSQL |
| **Delivery Guarantee** | Best-effort tick | Ephemeral delivery | At-least-once with lease | At-least-once with retry |
| **Retry & Backoff** | None (missed ticks skipped) | None | Exponential backoff + DLQ | Exponential backoff + audit log |
| **Idempotency** | N/A | None | Unique idempotency key | Unique event ID + deduplication |
| **Primary Use Cases** | Cache evictions, stats polling | Live SSE wakeup, presence sync | Emails, report exports, webhooks | Third-party webhooks, audit events |

---

## 3. Core Architecture: Durable Jobs Engine (`core/jobs`)

### 3.1 Job States and Transitions

A durable job transitions through a well-defined state machine:

```
                  +--------------+
                  |   PENDING    | <-----------------+
                  +--------------+                   |
                         | (Worker claims lease)     |
                         v                           |
                  +--------------+                   |
                  |   RUNNING    |                   |
                  +--------------+                   |
                    /          \                     |
   (Success)       /            \ (Error)            |
                  v              v                   |
          +-----------+    +-----------+             |
          | COMPLETED |    |  FAILED   | ------------+ (Retries remaining:
          +-----------+    +-----------+                re-queue with backoff)
                                 |
                                 | (Max retries exceeded)
                                 v
                           +-----------+
                           |   DEAD    | (Dead-Letter Queue)
                           +-----------+
```

1. **`pending`**: The job is enqueued and ready to be picked up by a worker when `run_at <= now()`.
2. **`running`**: A worker has acquired a lease on the job via `FOR UPDATE SKIP LOCKED`. A `leased_until` timestamp protects the job against worker crashes.
3. **`completed`**: The job finished successfully. Result payload is recorded; job is marked completed or purged per retention policy.
4. **`failed`**: The job encountered an error during execution. If `attempts < max_retries`, it transitions back to `pending` with exponential backoff (`run_at = now() + backoff`).
5. **`dead`**: All retry attempts exhausted. The job enters the Dead-Letter Queue (DLQ) for operator inspection, manual modification, or replay.

---

## 4. Transactional Enqueue Guarantees

Any job enqueued inside a database transaction (`app.RunInTransaction`):
- Is written to the `_jobs` table using the active transaction connection.
- If the outer business transaction rolls back, the job insert is rolled back simultaneously — **zero phantom jobs**.
- If the outer business transaction commits, the job is guaranteed to exist on disk and will be picked up by workers even if the server crashes immediately after commit.
