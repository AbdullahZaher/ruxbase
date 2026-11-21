# Wave 05: Realtime & Offline Synchronization — Event Boundaries, Ordering & Data-Loss Semantics

```
Document ID:     RUXBASE-WAVE05-EVENT-BOUNDARIES-AND-ORDERING
Version:         1.0.0
Milestone:       W05-01 (Declare committed-event boundaries, ordering, source-switch and data-loss semantics for existing SSE/WAL)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Milestone W05-01 defines the authoritative contract governing realtime event propagation across Ruxbase serving nodes, logical replication (WAL/CDC), and client subscriptions.

Ruxbase rejects unsupported "exactly-once" delivery claims in distributed environments. Instead, it enforces:
1. **Committed-Event Boundary**: Events are emitted exclusively after transaction commit. In-flight, aborted, or rolled-back transactions never produce subscriber events.
2. **Total Transactional Ordering**: Events are ordered by PostgreSQL commit Log Sequence Number (LSN) and in-transaction operation index.
3. **Deterministic Source Switching**: Documented fallback and recovery between `replication` (PostgreSQL logical decoding) and `hooks` (application-level lifecycle events).
4. **Data-Loss & Resync Semantics**: Strictly **at-least-once** internal delivery with node-level deduplication. Explicit handling for invalidated replication slots where clients must resync via REST.

---

## 2. Event Sources & Delivery Modes

Ruxbase supports two operational realtime event sources (`RUXBASE_REALTIME_SOURCE`):

| Source | Capture Mechanism | Trigger Point | Guarantees & Scope |
|---|---|---|---|
| `replication` (Recommended) | PostgreSQL logical replication (`pgoutput` via dedicated slot `ruxbase_rt_<db>`) | Decoded upon commit LSN confirmation | Captures all committed changes (API, direct SQL, external workers, ETL). |
| `hooks` (Default / Fallback) | Go lifecycle hooks (`OnRecordAfterCreateSuccess`, `OnRecordAfterUpdateSuccess`, etc.) | Post-commit hook execution | Captures changes made through Ruxbase API and server extensions. |

### Source-Switching Rules
- When configured for `replication`, Ruxbase preflights `wal_level = logical` and the `REPLICATION` role attribute.
- If unavailable, the leader logs a structured warning and switches dynamically to `hooks` mode.
- Re-check occurs every 60 seconds; once replication permissions and slot readiness are detected, the system transitions back to `replication`.
- During source switching, event IDs retain monotonic format to prevent duplicate processing by downstream subscriber nodes.

---

## 3. Committed-Event Boundary & Ordering Contract

### Rollback & Abort Invariant
```
BEGIN TRANSACTION;
  INSERT INTO tasks (title) VALUES ('Uncommitted draft');
ROLLBACK;
───> Result: 0 events emitted to SubscriptionBroker, 0 NOTIFY payloads.
```
In replication mode, PostgreSQL logical decoding streams changes only upon receiving `COMMIT` message. In hooks mode, events trigger strictly within `After*Success` handlers.

### Ordering Guarantee
- Within a single transaction: Events are tagged with `change_index` ($0, 1, 2, \dots, N-1$) in order of execution.
- Across transactions: Ordered strictly by commit LSN (`confirmed_flush_lsn`).
- Event ID Format: Derived deterministically from commit LSN and index, ensuring idempotency:
  $$\text{EventID} = \text{hex}(\text{LSN}) + \text{"\_"} + \text{index}$$

---

## 4. Data-Loss Semantics & Rejection of "Exactly-Once" Claims

Ruxbase explicitly rejects the claim of "exactly-once" delivery across network boundaries:
1. **At-Least-Once Delivery**: The cluster publishes event IDs over PostgreSQL `NOTIFY`. If an advisory leader disconnects, the next elected leader resumes from the confirmed LSN. Events decoded during the failover window are re-stored using `ON CONFLICT DO NOTHING`.
2. **Node Deduplication**: Subscriber nodes maintain a sliding-window deduplication cache (`_realtime_events` memory buffer) to discard redelivered events before sending to connected SSE clients.
3. **Slot Invalidation & Gap Recovery**:
   - If PostgreSQL invalidates a replication slot (e.g. `max_slot_wal_keep_size` exceeded due to long subscriber outage), the slot is recreated.
   - Any WAL segments purged in the interim represent missed events that cannot be recovered from the stream.
   - Clients detecting disconnection or reconnecting after expiration receive an explicit resync signal and must query the REST API (`GET /api/collections/:collection/records`) to reconcile state.
