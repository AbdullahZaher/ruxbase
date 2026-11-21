# Ruxbase Wave 02 — Versioned Migration Plans, Schema Drift & Immutable Identity

**Document Reference:** `docs/architecture/WAVE02-MIGRATION-DRIFT.md`  
**Milestone:** W02-06 / P0 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Architectural Mission & Invariants

Schema state must never be ambiguous, volatile, or untracked. Wave 02 establishes:
1. **Immutable Migration Identity:**
   - Every migration plan has a canonical version identifier and SHA-256 cryptographic checksum.
   - Once applied, the migration identity is recorded in `_migrations` and cannot be modified or replayed with altered contents.
   - Post-application tamper detection flags any discrepancies between applied history and code assets.
2. **Schema Drift Detection:**
   - Automated bidirectional reconciliation between the PostgreSQL physical catalog and Ruxbase metadata models.
   - Detects missing tables, untracked tables, missing/extra columns, data type divergences, and missing indexes.
3. **Deterministic Versioned Migration Plans:**
   - Explicit `Up` and `Down` DDL workflows executed under advisory transaction locks.
   - Transactional atomicity: schema transitions and migration ledger updates commit or abort together.

---

## 2. Drift Classification Taxonomy

| Drift Category | Severity | Description | Remediation |
|---|---|---|---|
| **`TABLE_MISSING`** | High | Collection exists in metadata but physical table is missing in PostgreSQL. | Recreate table or remove orphan collection metadata. |
| **`TABLE_UNTRACKED`** | Info | Physical PostgreSQL table exists but is not registered or exposed. | Expose via `ExposeNativeTable` or ignore if private. |
| **`COLUMN_MISSING`** | High | Field is defined in Collection but physical column does not exist. | Add column via `ALTER TABLE ... ADD COLUMN`. |
| **`COLUMN_EXTRA`** | Medium | Physical column exists in PostgreSQL table but is absent from Collection. | Map column to Collection field or drop unneeded column. |
| **`TYPE_MISMATCH`** | High | Physical column data type contradicts the Collection Field type. | Run typed migration to reconcile data type. |
| **`INDEX_MISSING`** | Low | Index defined in Collection is absent from PostgreSQL catalog. | Recreate index via `CreateNativeIndex`. |

---

## 3. Immutable Ledger & Tamper Detection Specification

1. **Checksum Calculation:**
   `SHA-256(UpSQL || DownSQL || Version)` generates a 64-character hexadecimal digest.
2. **Ledger Storage:**
   The `_migrations` table records:
   - `file`: Canonical version name (e.g. `20260330140000_create_billing.go` or `.sql`).
   - `applied`: Unix microsecond timestamp of application.
   - `checksum`: SHA-256 fingerprint of the executed migration.
3. **Tamper Verification:**
   Before running pending migrations, `VerifyMigrationIntegrity()` checks every applied migration against loaded plans. Discrepancies raise an immutable identity violation error.
