# Wave 01: Cluster Policy Revocation Consistency & Bounded Cache Guarantees

## 1. Executive Architecture Summary

This specification establishes the cluster policy cache invalidation and revocation consistency invariants for Ruxbase (Wave 01 Milestone M06: `W01-M06-01`, `W01-M06-02`, and `W01-M06-03`).

In a distributed multi-node Ruxbase deployment, multiple application instances share an external PostgreSQL 17/18 database. To maintain high throughput, application nodes maintain in-memory cached representations of collections, fields, and API access rules (`StoreKeyCachedCollections`). However, authorization security mandates that **revocation of privileges, deletion of collections, or restriction of access rules must take effect deterministically across all nodes**, with guaranteed bounds on propagation and strict fail-closed behavior during network partitions or disconnections.

```
       Node 1 (Mutator)                      PostgreSQL (Authority)                    Node 2 (Follower)
┌──────────────────────────────┐        ┌──────────────────────────────┐        ┌──────────────────────────────┐
│ Admin / Scoped Mutation      │        │                              │        │ In-Memory Policy Cache       │
│ • Update Collection Rule     │───────>│ 1. Transaction Commit        │        │ • Old Rule in Cache          │
│ • Drop / Alter RLS Policy    │        │    - _collections row updated│        │                              │
│ • OnCollectionAfterSuccess   │        │    - pg_policy updated (RLS) │        │                              │
│ • Publish cluster topic      │───────>│ 2. pg_notify(channel, sig)   │───────>│ 3. LISTEN notification rx    │
│                              │        │                              │        │    - verify fingerprint      │
│                              │        │                              │        │    - reload collection cache │
└──────────────────────────────┘        └──────────────────────────────┘        └──────────────────────────────┘
                                                 ▲
                                                 │
                                        Direct SQL / RLS Query
                                        (Evaluates at engine level:
                                         ZERO-STALENESS T=0)
```

---

## 2. Declared Revocation Invariants & Timing Semantics

### Invariant 1: Engine-Level RLS Zero-Staleness ($T_{\text{staleness}} = 0$)
Row-Level Security (RLS) policies are stored directly within PostgreSQL catalog tables (`pg_policy`, `pg_class`). When an access rule or collection definition is modified via `SaveCollection` or `applyRLSDefinition`, the DDL statements (`ALTER TABLE ... ENABLE ROW LEVEL SECURITY`, `DROP POLICY`, `CREATE POLICY`) execute and commit inside the database transaction.

- **Guarantee:** As soon as the transaction commits on PostgreSQL, **any query executed via PostgreSQL RLS across ANY node in the cluster immediately evaluates against the updated policy**.
- **Defense-in-Depth:** Even if an application node's in-memory memory cache is transiently delayed, direct SQL writes, scoped transaction operations (`RunInScopedTransaction`), or RLS-enforced operations cannot execute a revoked operation. The database engine strictly denies unauthorized rows ($0$ rows affected or SQLSTATE 42501).

### Invariant 2: Sub-Second Cluster Bus Invalidation ($T_{\text{invalidation}} \le 100\text{ms}$)
Under normal connected cluster operations:
1. When a collection is created, updated, or deleted on Node 1, `OnCollectionAfterCreateSuccess`, `OnCollectionAfterUpdateSuccess`, or `OnCollectionAfterDeleteSuccess` executes.
2. An HMAC-SHA256 signed cluster notification is published on the PostgreSQL LISTEN/NOTIFY channel under topic `collections`.
3. Subscribed peer nodes (Node 2, Node 3) receive the notification via their dedicated LISTEN connection.
4. The receiving node calculates the cluster state fingerprint:
   $$\text{MD5}(\text{string\_agg}(c::\text{text}, ',' \text{ ORDER BY } c.\text{id}))$$
5. If the fingerprint has changed, `app.ReloadCachedCollections()` is invoked immediately, atomically replacing `StoreKeyCachedCollections`.

### Invariant 3: Partitioned & Disconnected Fail-Closed Fallback
When a node suffers a network partition, database connection interruption, or temporary failure of its dedicated LISTEN connection:
1. `Cluster.Connected()` returns `false`.
2. The node recognizes that it is disconnected from the cluster notification bus and cannot receive asynchronous invalidation events.
3. In `core/collection_query.go`:
   - `FindCachedCollectionByNameOrId` detects `app.cluster.Enabled() && !app.cluster.Connected()`.
   - The node immediately **bypasses the in-memory cache and falls back to a live database query** (`FindCollectionByNameOrId`).
   - `FindCachedCollectionReferences` similarly falls back to `FindCollectionReferences`.
4. **Guarantee:** A disconnected or partitioned node never uses an un-invalidated, potentially stale in-memory cache to grant an access right that was revoked during the partition window.

### Invariant 4: Reconnect & Resynchronization Convergence
When a disconnected node reconnects to PostgreSQL:
1. `Cluster.listen()` successfully establishes the dedicated `pgx` connection and sends `LISTEN`.
2. `c.connected.Store(true)` marks the node as connected.
3. `c.requestResync()` is triggered immediately.
4. The `OnResync` hooks execute `reloadCollections(true)` and `reloadSettings(true)`.
5. The node compares its local fingerprint with the live PostgreSQL catalog fingerprint. If any changes occurred during the partition, the in-memory cache is fully refreshed.

---

## 3. Realtime Authentication Session Revocation

In addition to collection rules, user identity and token revocation are synchronized across nodes:
- When an auth collection secret is refreshed or modified (`AuthToken.Secret`), `realtimeNotifyAuthCollectionChange` publishes `rt_auth_collection`. Peer nodes call `realtimeUnsetClientsAuthByCollection` to disconnect and de-authenticate all connected SSE subscribers for that collection.
- When an individual auth record's `tokenKey` is refreshed (e.g., password reset, explicit logout, session revocation), Node 1 emits `clusterTopicRealtimeRecord`. Peer nodes invoke `realtimeUpdateClientsAuth`, comparing `clientAuth.TokenKey() != authRecord.TokenKey()`, immediately unsetting the client's authentication.
- When an auth record is deleted, `realtimeUnsetClientsAuthByRecordModelOrProxy` unsets client authentication across all cluster nodes.

---

## 4. Verification & Testing Matrix

| Scenario | Objective | Verification Mechanism | Status |
|---|---|---|---|
| Connected Invalidation | Node 1 revokes rule; Node 2 receives NOTIFY and reloads cache | `TestClusterPolicyRevocation_ConnectedPropagation` | PASS |
| Engine Zero-Staleness | Node 2 cache artificially held stale; PostgreSQL RLS blocks write | `TestClusterPolicyRevocation_RLSZeroStalenessUnderStaleAppCache` | PASS |
| Partition Fail-Closed | Node 2 LISTEN socket down; `FindCached` falls back to DB | `TestClusterPolicyRevocation_PartitionedNodeFailClosed` | PASS |
| Reconnect Resync | Partitioned node reconnects and synchronizes missing updates | `TestClusterPolicyRevocation_ReconnectResyncConvergence` | PASS |
| Auth Revocation | User tokenKey change unsets auth on peer cluster node | `TestClusterPolicyRevocation_AuthTokenKeyRevocation` | PASS |
