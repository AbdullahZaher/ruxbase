# Ruxbase Wave 02 — Online Schema Evolution Lifecycle (Expand → Backfill → Validate → Contract)

**Document Reference:** `docs/architecture/WAVE02-ONLINE-SCHEMA-LIFECYCLE.md`  
**Milestone:** W02-07 / P0 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Architectural Mission & Invariants

Production databases cannot tolerate table rewrites or indefinite table locks that block active read/write traffic.
Wave 02 defines the formal **Expand → Backfill → Validate → Contract** online schema evolution lifecycle:
1. **Bounded Locks:** Every DDL operation executes with an explicit `lock_timeout` (default 2s) and `statement_timeout` (default 10s). If lock acquisition is blocked by background queries, the transaction aborts cleanly rather than queuing and starving subsequent queries.
2. **Chunked Backfill:** Historical data backfill operates in bounded primary-key slices (`LIMIT :batchSize`) to avoid holding long row locks or causing replication lag.
3. **Non-Blocking Constraints:** Constraints are added with `NOT VALID` (instantaneous catalog update) and subsequently validated with `VALIDATE CONSTRAINT` under non-blocking `SHARE UPDATE EXCLUSIVE` locks.
4. **Deterministic Rollback & Forward-Fix:** Every phase defines its deterministic rollback step until the Contract phase is executed.

---

## 2. The Four-Phase Evolution Model

```
 ┌─────────────┐       ┌──────────────┐       ┌──────────────┐       ┌──────────────┐
 │   EXPAND    │  ───► │   BACKFILL   │  ───► │   VALIDATE   │  ───► │   CONTRACT   │
 └─────────────┘       └──────────────┘       └──────────────┘       └──────────────┘
  Add new column/       Chunked batch          Validate               Drop deprecated
  table (nullable       backfill of            constraints with       old column or
  or with default)      historical data        SHARE UPDATE EXCL      triggers
```

### 2.1 Stage 1 — Expand
- Add the new column as `NULLABLE` (or with default value, which is metadata-only in modern PostgreSQL).
- Application dual-writes to both the old and new representations.
- Rollback: `ALTER TABLE ... DROP COLUMN ...`.

### 2.2 Stage 2 — Backfill
- Execute batch updates in primary-key ranges (`WHERE id > :cursor ORDER BY id LIMIT :batchSize`).
- Each batch commits independently to release row locks and allow concurrent queries to interleave.
- Rollback: Revert data transformation or abort remaining batches.

### 2.3 Stage 3 — Validate
- Add constraints using `ALTER TABLE ... ADD CONSTRAINT ... NOT VALID`.
- Validate constraints asynchronously using `ALTER TABLE ... VALIDATE CONSTRAINT ...`.
- Verify parity between old and new data.
- Rollback: `ALTER TABLE ... DROP CONSTRAINT ...`.

### 2.4 Stage 4 — Contract
- Point of no return.
- Application code has transitioned to read and write exclusively from the new column.
- Deprecated old column, legacy views, or synchronization triggers are dropped.
- Recovery policy: Forward-fix only.
