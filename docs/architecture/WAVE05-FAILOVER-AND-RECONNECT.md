# Wave 05 — Milestone 04: Node Failover, PostgreSQL Reconnect & Slot Recovery

## 1. Executive Summary & Objective

In multi-node PostgreSQL-native environments, database connection interruptions, leader process crashes, network partitions, and replication slot drops are inevitable operational realities. The real-time engine must sustain continuous operations without split-brain leadership, unhandled reconnections, or duplicated event delivery.

Milestone **W05-04 / P1 / CORE** documents and verifies the failover, reconnection, slot recovery, and duplicate filtering semantics across Ruxbase cluster nodes.

---

## 2. Core Architecture & Mechanisms

### 2.1 Advisory Lock Leader Election & Failover
- **Lock Identifier:** `LockKey("realtime_replication_leader")`, evaluated via 64-bit FNV hashing to ensure deterministic isolation per database.
- **Acquisition:** Competing cluster nodes attempt non-blocking session-level lock acquisition using `SELECT pg_try_advisory_lock($1)` on a dedicated connection.
- **Failover Semantics:**
  - When the active leader node crashes, terminates, or closes its connection, PostgreSQL automatically releases all session-level advisory locks held by that session.
  - Competing standby nodes run election checks every `leaderRetryInterval` (2 seconds).
  - Exactly one standby node acquires the lock and transitions to active leader (`c.replicationLeader.Store(true)`), resuming replication stream consumption.
  - Idle timeout protection (`idle_session_timeout = 60s`) ensures that even if an OS freezes without socket reset, PostgreSQL terminates the abandoned session.

### 2.2 PostgreSQL LISTEN Reconnection with Exponential Backoff
- The cluster pub/sub bus uses a dedicated PostgreSQL connection running `LISTEN ruxbase_cluster_<secret>`.
- If the connection drops or fails with a network error:
  1. The node enters an exponential backoff reconnect loop starting at `minReconnectDelay` (100ms) doubling up to `maxReconnectDelay` (10s).
  2. Once the physical connection is re-established, the channel is re-subscribed via `LISTEN`.
  3. The node fires `Cluster.OnResync` handlers to refresh internal collection definitions and caches that may have drifted during the disconnect window.

### 2.3 Replication Slot Invalidation & Recovery
- The replication engine utilizes PostgreSQL logical decoding (`pgoutput`).
- If an existing replication slot is invalidated or dropped:
  1. The leader detects the slot failure on connection startup (`pglogrepl.StartReplication`).
  2. The leader invokes `CreateReplicationSlot` with temporary or persistent slot naming (`c.replicationSlot`).
  3. `ensurePublication` verifies that all target collections exist in the publication with `REPLICA IDENTITY FULL`.
  4. Logical streaming resumes from the last confirmed LSN.

### 2.4 Duplicate Event Filtering & Deduplication
- Because PostgreSQL WAL streaming operates under "at least once" delivery across leader failovers or restart boundaries, duplicate change events can be emitted.
- Ruxbase nodes deploy `cluster.EventsDeduplicator` with a sliding memory window (capacity: 10,000 entries).
- When a node receives an event ID batch on `cluster.TopicRealtimeEvents`, `dedup.Filter(ids)` strips out any already-processed event IDs before dispatching to local subscribers.

---

## 3. Verification Matrix

| Test Scenario | Subsystem | Verification Criterion | Status |
|---|---|---|---|
| **Leader Failover** | Replication Engine | When leader shuts down, standby acquires advisory lock within 2.5s and consumes stream | PASS |
| **LISTEN Reconnect** | Cluster Bus | Node re-establishes LISTEN connection after connection termination and resumes pub/sub | PASS |
| **Duplicate Filtering** | Event Deduplicator | Replayed event IDs across failover boundaries are filtered; subscribers receive events once | PASS |
| **Slot Recovery** | PostgreSQL CDC | Missing/dropped logical replication slot is automatically recreated and streaming resumes | PASS |
