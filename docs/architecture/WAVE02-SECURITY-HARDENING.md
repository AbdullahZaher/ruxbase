# Ruxbase Wave 02 — SQL & Routine Security Hardening

**Document Reference:** `docs/architecture/WAVE02-SECURITY-HARDENING.md`  
**Milestone:** W02-09 / P0 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Threat Model & Architectural Mission

Exposing database routines, views, and SQL capabilities introduces critical attack surfaces:
1. **Privilege Escalation via `SECURITY DEFINER`:**
   A function marked `SECURITY DEFINER` runs with the owner's privileges (often a superuser or administrative role). If an attacker can execute such a function with arbitrary input, they can bypass table-level RLS or manipulate restricted state.
   - **Remediation:** Ruxbase prohibits public exposure of `SECURITY DEFINER` functions without explicit administrator authorization (`AllowSecurityDefiner: true`).
2. **RLS Bypass via Views:**
   Standard PostgreSQL views execute with the view owner's privileges, bypassing Row-Level Security policies of the calling user.
   - **Remediation:** All views generated or managed by Ruxbase enforce `WITH (security_invoker = true)` so caller credentials and policies govern underlying table access.
3. **Execution of Dangerous Built-in Functions:**
   PostgreSQL contains administrative built-in functions (e.g. `pg_read_file`, `pg_terminate_backend`, `lo_export`, `dblink`).
   - **Remediation:** Strict blacklist blocks introspection, exposure, or execution of dangerous built-ins.
4. **Automated Routine Security Auditing:**
   A dedicated security auditor scans the PostgreSQL catalog for unsafe views, unvetted `SECURITY DEFINER` routines, and dangerous routines in non-system schemas.

---

## 2. Dangerous Built-in Blacklist

| Routine Category | Functions | Enforcement |
|---|---|---|
| **Filesystem Access** | `pg_read_file`, `pg_read_binary_file`, `pg_write_file`, `lo_import`, `lo_export` | Blocked unconditionally |
| **Backend & Cluster Control** | `pg_terminate_backend`, `pg_cancel_backend`, `pg_reload_conf`, `pg_rotate_logfile` | Blocked unconditionally |
| **Remote Connections** | `dblink`, `dblink_exec` | Blocked unconditionally |
| **System Catalogs** | Any routine in `pg_catalog` or `information_schema` | Blocked from exposure |

---

## 3. Routine Security Audit Report Specification

The `AuditDatabaseRoutineSecurity` method returns:
- `SecurityDefinerFunctions`: Non-system routines configured with `prosecdef = true`.
- `MissingSecurityInvokerViews`: Views lacking `security_invoker = true`.
- `DangerousRoutinesFound`: Any blacklisted functions located outside system schemas.
- `IsSecure`: Boolean indicator reporting whether any high-severity security findings remain open.
