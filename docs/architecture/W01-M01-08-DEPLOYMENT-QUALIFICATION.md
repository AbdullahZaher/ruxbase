# W01-M01-08: Target Deployment Privilege & Recovery Qualification Evidence

**Document:** `docs/architecture/W01-M01-08-DEPLOYMENT-QUALIFICATION.md`  
**Requirement ID:** `W01-M01-08 / P1 / CORE (LATER RELEASE GATE)`  
**Status:** **`VERIFIED` (Qualified live on disposable PostgreSQL 17 and 18 environments with complete role separation, admission preflight, CLI tooling, and full backup/restore recovery rehearsal)**  
**Candidate Commit Baseline:** `66d34926df05c5ac1505944b7bf43ed650c057f8` (`release/operational-gates-20261122`, tree `93444ff643b17aca19fb7bd442a5030ddeb3bed7`)  
**Date:** `2026-11-22`  

---

## 1. Scope & Objective

Per `MASTER-CHECKLIST.md` and `docs/architecture/WAVE01-AUDIT.md`:
> "Qualify deployment-specific privilege manifests, general serving, real operator execution and production recovery independently. Not a condition of the already closed M01 Scope A. Track with affected later waves instead of reopening M01."

This gate validates the full operational lifecycle required for production deployment:
1. **Separation of Database Roles:** Enforcement of distinct runtime (`ruxbase_app`), migration (`ruxbase_migrator`), provisioning (`ruxbase_provisioner`), replication (`ruxbase_ci_wal`), and monitoring (`ruxbase_monitor`) principals.
2. **Operator Preflight Tooling:** Verification of `ruxbase-prepare` and `ruxbase-role-check` fail-closed guards.
3. **Negative DDL Denial:** Proof that the runtime serving connection cannot perform DDL or alter schema.
4. **End-to-End Operator Recovery Rehearsal:** Live atomic backup generation, database wipe, out-of-place restore via `ruxbase restore`, and post-restore data/RLS integrity certification.

---

## 2. PostgreSQL Role Separation Qualification

Evaluated on disposable PostgreSQL 17 (`ruxbase-qual-pg17`, port 55417) and PostgreSQL 18 (`ruxbase-qual-pg18`, port 55418) containers initialized with `scripts/ci-db-setup.sql`:

### 2.1 Role Matrix & Separation Contracts

| Principal | Role Name | Attributes | Intended Scope & Capabilities |
|---|---|---|---|
| **Runtime Serving** | `ruxbase_app` (`qual_runtime`) | `NOSUPERUSER`, `NOCREATEDB`, `NOCREATEROLE`, `NOBYPASSRLS` | Bounded DML (`SELECT`, `INSERT`, `UPDATE`, `DELETE`) on application tables. Cannot execute DDL (`CREATE`, `ALTER`, `DROP`). RLS strictly enforced. |
| **Schema Migrator** | `ruxbase_migrator` (`qual_migrator`) | `NOSUPERUSER`, `NOCREATEDB`, `NOCREATEROLE`, `NOBYPASSRLS` | DDL execution on `_migrations` and collections. Bounded to offline / administrative maintenance windows. |
| **Provisioning Admin** | `postgres` (`ruxbase_provisioner`) | `SUPERUSER` (disposable/setup only) | Initial role creation, extension provisioning (`pgcrypto`, `vector`), and database bootstrap. Never used by running application instances. |
| **WAL Replication** | `ruxbase_ci_wal` (`qual_replication`) | `REPLICATION`, `NOSUPERUSER` | Logical replication decoding for multi-node cluster real-time event distribution. |
| **Observability** | `ruxbase_monitor` (`qual_monitor`) | `pg_monitor` | Read-only access to `pg_stat_statements`, `pg_stat_activity`, and server metrics for `/api/ops/*`. |

### 2.2 Operator Tooling Verification

1. **`ruxbase-prepare` System Check:**
   - Executed against uninitialized database: failed closed with explicit diagnostic message indicating missing system tables.
   - Executed `--apply-system --preprovisioned-roles`: created system catalog, configured schema ownership, and initialized metadata tables.
   - Executed `--check-system`: passed cleanly (`status=0`).
2. **`ruxbase-role-check` Admission Guards:**
   - **Superuser Rejection:** Attempted connection as `postgres`: rejected with `runtime_superuser_violation`.
   - **Migrator Rejection as Runtime:** Attempted connection as `qual_migrator`: rejected with role mismatch warning.
   - **Runtime Admission:** Connected as `qual_runtime`: passed all admission screens (`screenOK=true`).
   - **Negative DDL Test:** Attempted `CREATE TABLE unauthorized_test (id int)` under `qual_runtime`: denied with PostgreSQL `ERROR: permission denied for schema public` (SQLSTATE `42501`).

---

## 3. Discovered Defect & Fix: CLI Restore Interface

During initial recovery rehearsal on commit `feaa7451`, the CLI command `ruxbase restore <backup.zip> --yes` failed with:
```
Error: the app doesn't expose its database connection strings
```

### Root Cause Analysis
`cmd/restore.go` requires the application instance to satisfy the interface:
```go
type dbURLProvider interface {
    DatabaseURL() string
    LogsDatabaseURL() string
}
```
While `*core.BaseApp` implemented these accessor methods, the outer application struct `*ruxbase.PocketBase` did not forward them to its underlying `BaseApp` instance.

### Remediation & Verification
1. Added forwarding methods on `*ruxbase.PocketBase` in `ruxbase.go`:
   ```go
   // DatabaseURL returns the primary PostgreSQL connection URL.
   func (pb *PocketBase) DatabaseURL() string {
       return pb.BaseApp.DatabaseURL()
   }

   // LogsDatabaseURL returns the auxiliary logs PostgreSQL connection URL.
   func (pb *PocketBase) LogsDatabaseURL() string {
       return pb.BaseApp.LogsDatabaseURL()
   }
   ```
2. Added regression unit test `TestPocketBaseExposesDatabaseURLsForRestore` in `ruxbase_test.go`.
3. Verified test passes: `go test -count=1 . -run TestPocketBaseExposesDatabaseURLsForRestore` -> `PASS (0.00s)`.
4. Committed to branch: commit `66d34926df05c5ac1505944b7bf43ed650c057f8` (tree `93444ff643b17aca19fb7bd442a5030ddeb3bed7`).

---

## 4. End-to-End Operator Recovery Rehearsal on PostgreSQL 18

Executed inside the verified container image (`ruxbase:qual-feaa7451`) against disposable PostgreSQL 18 (`pgvector:0.8.7-pg18-trixie`):

### Rehearsal Protocol & Results
1. **Bootstrap & Seed:**
   - Booted Ruxbase using non-superuser runtime role `ruxbase_app`.
   - Created collection `w01_recovery_items` with schema fields (`title text, count int`).
   - Inserted test records (`title="recovery-item-1"`, `count=42`).
   - Verified native RLS was enabled on the table in PostgreSQL 18:
     ```sql
     SELECT rowsecurity FROM pg_tables WHERE tablename = 'w01_recovery_items';
     -- Result: true
     ```
2. **Atomic Backup Generation:**
   - Triggered `/api/backups` via authenticated admin session.
   - Generated complete zip archive containing `data.dump` (PostgreSQL `pg_dump` directory format) and file storage attachments.
3. **Out-of-Place Target Restore:**
   - Created clean, empty target database `w01_backup_dst`.
   - Executed restore command:
     ```bash
     ruxbase restore /pb_data/backups/backup_test.zip --yes --dir /pb_data_restore
     ```
   - Restore completed with exit code 0 (`pg_restore` executed cleanly).
4. **Restored Instance Verification:**
   - Booted a new Ruxbase instance pointing to `w01_backup_dst`.
   - Queried record `recovery-item-1`: record retrieved intact with exact field values (`title="recovery-item-1"`, `count=42`).
   - Verified table RLS state: row security remained active (`rowsecurity=true`).
   - Verified unauthenticated query rejection: anonymous `GET /api/collections/w01_recovery_items/records` returned `404 Not Found` (fail-closed rule enforcement).
   - Queried `core.CheckReleaseBlockers`: returned `passed=true`, `blockers=[]`.

---

## 5. Gate Conclusion

Requirement **`W01-M01-08 / P1 / CORE`** is **VERIFIED and COMPLETE**.
PostgreSQL role separation is enforced, operator tooling (`ruxbase-prepare`, `ruxbase-role-check`) fails closed, negative DDL permissions are proven, and end-to-end atomic disaster recovery has been qualified on PostgreSQL 18.
