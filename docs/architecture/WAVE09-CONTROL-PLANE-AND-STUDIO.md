# Wave 09: Ruxbase Studio & Optional Multi-Project Control Plane

**Document:** `docs/architecture/WAVE09-CONTROL-PLANE-AND-STUDIO.md`  
**Target Wave:** Wave 09 — Control Plane & Ruxbase Studio  

---

## 1. Embedded Ruxbase Studio Operational Views (W09-01, W09-02)

Ruxbase embeds the single-project administrative Studio directly into the server binary (`ui/dist/`):
- **Collections, PostgreSQL SQL Console, Storage, Backups, Auth & Crons:** Preserved and integrated under superuser authentication.
- **Operations & Studio (`#/settings/operations`):** Added in `ui/src/settings/operations/pageOperations.js` backed by `/api/studio/overview`, `/api/studio/jobs`, `/api/studio/audit`, and `/api/control-plane/projects`.
  - Surfaces applied migrations, collections count, PostgreSQL backends, storage bucket byte totals, active alerts, durable job queue inspection, one-click Dead-Letter Queue job replay, and immutable audit logs.

---

## 2. Administrative RBAC & Immutable Audit Trail (W09-03)

- **Strict Superuser Enforcement:** All `/api/studio/*` and `/api/control-plane/*` endpoints require `apis.RequireSuperuserAuth()`. Unauthenticated requests and normal collection auth users are rejected with `401`/`403`.
- **Immutable Audit Ledger (`_admin_audit_logs`):** Security-sensitive mutations (replaying dead jobs, registering control plane projects, transitioning project lifecycles) atomically record `actor_id`, `actor_email`, `action`, `resource`, `target_id`, `ip_address`, `details` JSONB, and timestamp alongside structured `slog` output.

---

## 3. Optional Self-Hosted Multi-Project Control Plane (W09-04, W09-05)

Single-project Ruxbase installs operate independently with zero control plane dependencies or configuration. Operators running multiple self-hosted projects can register projects in `_control_plane_projects`:
- **Per-Project Boundary Invariant:** Every project MUST specify a unique `slug`, unique `db_name`, and unique least-privilege `db_role`.
- **Privileged Role Rejection:** Attempting to bind a project to shared system/privileged roles (`postgres`, `admin`, `ruxbase_app`, `ruxbase_migrator`, `ruxbase_provisioner`, `ruxbase_anon`, `ruxbase_authenticated`, or `pg_*`) is rejected at admission (`ErrControlPlanePrivilegedRoleViolation`).
- **Quotas & Lifecycle:** Tracks `environment` (`development`, `staging`, `production`), `status` (`active`, `suspended`, `archived`), `storage_quota_bytes`, and `max_connections`.
