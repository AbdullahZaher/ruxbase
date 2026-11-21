# Wave 05 — Milestone 02: Subscription Authorization, Multi-Node Token Revocation & Realtime Access Boundary

## 1. Executive Summary & Objective

In self-hosted multi-node Backend-as-a-Service environments, real-time message distribution creates acute authorization challenges. Long-lived streaming transports (Server-Sent Events / SSE) maintain persistent HTTP connections for up to 30 minutes. If authorization state is only verified at connection establishment, revoked sessions, rotated credentials, deleted accounts, altered tenant boundaries, or updated access rules can leak sensitive database changes across the cluster.

Milestone **W05-02 / P0 / CORE** establishes an authoritative, defense-in-depth subscription authorization and revocation architecture for Ruxbase. It enforces two complementary guarantees:
1. **Instant Multi-Node Push Revocation ($T_0 \le 5\text{ms}$):** When an authenticated identity's credential (`tokenKey`), password, active role, or existence changes on Node A, a cluster-wide revocation event (`rt_auth_revoke`) is broadcast across PostgreSQL `LISTEN`/`NOTIFY`. Every receiving node (Node B..N) immediately evicts the client's cached auth context.
2. **Authoritative Delivery-Time Verification (Pull Defense-in-Depth):** During record broadcast evaluation (`realtimeBroadcastRecordWithAccessCheck`), before evaluating private collection rules for any connected client, the node verifies the client's `tokenKey` against the authoritative database state. If the session has been revoked, rotated, or deleted, the client is stripped of auth context (`client.Unset(RealtimeClientAuthKey)`) and demoted to an unauthenticated guest before rule execution.
3. **Cross-Node Multi-Tenant Isolation:** Real-time events for tenant-scoped collections (`tenant_id` column present) are checked against `app.FilterTenantRealtimeEvent(principal, recordTenant)`. A client authenticated under Tenant A is mathematically blocked from receiving events generated for Tenant B across all cluster nodes.
4. **Dynamic Rule & Negative Predicate Enforcement:** When collection API rules change from public to private, or when a record update alters field values such that the client's subscription filter (e.g. `status = 'pending'`) or access rule (e.g. `@request.auth.id = author`) no longer matches, real-time event delivery fails closed and drops the message.

---

## 2. Multi-Node Revocation Architecture

```
+-----------------------------------------------------------------------------------+
|                                  PostgreSQL                                       |
|                  Channel: ruxbase_cluster_<secret> (LISTEN/NOTIFY)                |
+-----------------------------------------------------------------------------------+
             ^                                                        |
             | NOTIFY "rt_auth_revoke|users|usr123"                  | LISTEN (async)
             |                                                        v
+-----------------------------+                           +-----------------------------+
|           NODE A            |                           |           NODE B            |
|  (User password rotated /   |                           | (Connected SSE Subscribers) |
|   session revoked at T0)    |                           |                             |
|                             |                           | 1. On rt_auth_revoke:       |
| 1. Model update / delete hook|                           |    Unset RealtimeClientAuth |
| 2. notify("rt_auth_revoke") |                           | 2. On Broadcast Record:     |
| 3. Unset local clients auth |                           |    Validate tokenKey in DB  |
|                             |                           |    Drop private events      |
+-----------------------------+                           +-----------------------------+
```

### 2.1 Push Invalidation Flow (`rt_auth_revoke`)
- **Trigger:** An auth record update (`OnModelAfterUpdateSuccess`), auth record deletion (`OnModelAfterDeleteSuccess`), or explicit session revocation (`RevokeUserSession`, `RefreshTokenKey`).
- **Cluster Broadcast:** In cluster mode, the node publishes to `clusterTopicRealtimeAuthRevoke` (`rt_auth_revoke`) with payload `<collectionName>|<recordId>`.
- **Node Consumption:** All cluster subscriber nodes receive the message, identify any local clients in `SubscriptionsBroker` whose `clientAuth.Id == recordId`, and call `client.Unset(RealtimeClientAuthKey)`.
- **Result:** The client connection remains open (preventing unnecessary TCP/TLS reconnect churn), but its authorization state is wiped. Any subsequent message evaluation evaluates rules under unauthenticated guest context (`Auth: nil`).

### 2.2 Authoritative Delivery-Time Validation (Pull Verification)
- In high-load or partitioned scenarios where a cluster notification might experience queue lag, event delivery must not rely solely on push notifications.
- In `realtimeBroadcastRecordWithAccessCheck`:
  ```go
  if clientAuth != nil && !clientAuth.IsSuperuser() {
      currentAuth, err := accessCheckApp.FindRecordById(clientAuth.Collection().Id, clientAuth.Id)
      if err != nil || currentAuth == nil || currentAuth.TokenKey() != clientAuth.TokenKey() {
          client.Unset(RealtimeClientAuthKey)
          clientAuth = nil
      } else {
          clientAuth = currentAuth
          client.Set(RealtimeClientAuthKey, currentAuth)
      }
  }
  ```
- If `currentAuth.TokenKey() != clientAuth.TokenKey()`, the cached credential was invalidated:
  1. Auth state is unset on the client.
  2. `clientAuth` is nulled for the current broadcast.
  3. Private rule checks (`@request.auth.id != ''`) evaluate to `false`.
  4. The event is safely dropped.

---

## 3. Multi-Tenant Realtime Isolation Contract

When a client establishes an SSE subscription via `POST /api/realtime`, tenant identity is captured from:
1. The authenticated token claims (`tenantId` claim in `VerifyTenantAuthToken`).
2. The `X-Tenant-Id` HTTP header.
3. The auth record's default `tenant_id` attribute.

The active tenant identifier is stored under `RealtimeClientTenantKey` (`pbTenantId`) on the client session.

### 3.1 Tenant Record Event Filtering
When a record event is broadcast:
- If the collection schema contains the `tenant_id` field:
  - If `clientAuth == nil` (guest): **DENIED**. Unauthenticated guests cannot subscribe to or receive tenant-scoped real-time updates.
  - If `clientAuth != nil && !clientAuth.IsSuperuser()`:
    - If `clientTenant == recordTenant`: **ALLOWED** (subject to standard collection access rules).
    - If `clientTenant != recordTenant`: The system verifies whether the user holds an active membership in `recordTenant` via `app.GetTenantMembership(recordTenant, clientAuth.Id)`. If not active, event delivery is **DENIED**.

---

## 4. Verification and Acceptance Matrix

| Scenario | Target | Expected Behavior | Disposition |
|---|---|---|---|
| **Local Token Rotation** | Client on Node A | Password reset / `tokenKey` change stops private event delivery immediately at $T_0$. | PASS |
| **Cross-Node Token Revocation** | Client on Node A, Revocation on Node B | Node B emits `rt_auth_revoke`; Node A clears client auth; Node A drops subsequent private events. | PASS |
| **Cross-Node Account Deletion** | Client on Node A, User Deleted on Node B | User record deleted on Node B; Node A evicts client auth; private events dropped. | PASS |
| **Tenant Event Isolation** | Client in Tenant A on Node A, Record created in Tenant B on Node B | Client on Node A receives zero messages for Tenant B; client in Tenant B receives event. | PASS |
| **Rule Hardening (Public -> Private)** | Guest client on Node A, Rule updated on Node B | `ListRule` changed from `""` to `@request.auth.id != ''`; guest immediately stops receiving events. | PASS |
| **Negative Filter Predicate** | Client subscribed with `status = 'pending'` | Record updated to `status = 'completed'`; client receives no update message. | PASS |
| **Ownership Transfer** | Rule `@request.auth.id = author` | Author updated from `usrA` to `usrB`; `usrA` client receives no update message. | PASS |
