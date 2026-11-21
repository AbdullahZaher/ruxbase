# Wave 01 Milestone M04 — Transaction Context & Tenant Isolation Lifecycle

This document provides the authoritative engineering specification and evidence ledger for **Wave 01 Milestone M04: Transaction Context & Tenant Isolation Lifecycle** (W01-M04-01, W01-M04-02, W01-M04-03).

---

## 1. Overview & Isolation Architecture

Ruxbase applications execute against external PostgreSQL connection pools where database connections are shared, multiplexed, and reused across concurrent HTTP requests, background workers, and tenant operations. Without strict transaction-local boundaries, multiplexed connections risk leaking session variables, user tokens, tenant IDs, or switched roles across request boundaries.

Milestone M04 establishes a hardened, context-aware, transaction-scoped execution model:
- **`core.TransactionScope`**: A typed specification for declaring principal, role, rule, tenant identifier, token lifetime, and arbitrary claims.
- **`app.RunInScopedTransaction`** & **`app.RunInScopedTransactionWithContext`**: Transaction wrappers that enforce fail-closed role switching and guarantee that all settings are transaction-local (`is_local = true`).
- **Zero-Leakage Pool Contract**: When a transaction terminates (via `COMMIT`, `ROLLBACK`, or context cancellation), PostgreSQL automatically and synchronously reverts all session configuration to the connection baseline. No tenant state, authentication token, or switched role persists on the returned pool connection.

---

## 2. Transaction Scope Specification

```go
type TransactionScope struct {
    // Principal is an optional authenticated auth record.
    Principal *Record

    // Role specifies the PostgreSQL role to switch to for the duration of the transaction.
    // If empty and Principal is provided, defaults to RLSAuthenticatedRole.
    Role string

    // Rule optionally sets the RLSRuleSetting (e.g. RLSRuleList or RLSRuleView).
    Rule string

    // TenantID is an optional tenant identifier bound locally to ruxbase.tenant_id for the transaction.
    TenantID string

    // TokenDuration specifies the duration of the generated RLS token (defaults to 1 minute).
    TokenDuration time.Duration

    // Claims specifies additional transaction-local configuration settings.
    Claims map[string]string

    // ReadOnly specifies whether the transaction should execute with READ ONLY semantics.
    ReadOnly bool
}
```

### Invariant Rules
1. **Safe Role Switching:**
   - Role switching is enacted strictly via `set_config('role', role, true)`.
   - Elevated roles (`postgres`, `admin`, `superuser`, or any role containing `superuser`) are rejected before transaction execution begins.
   - Superuser records cannot be passed as RLS principals because superusers bypass PostgreSQL Row-Level Security.
2. **Transaction-Local Setting Enforcement:**
   - All settings (`ruxbase.token`, `ruxbase.rule`, `ruxbase.tenant_id`, and custom claims) use `set_config(name, value, true)`.
   - Because `is_local = true`, the scope of the parameter is strictly limited to the transaction block. Upon `COMMIT` or `ROLLBACK`, PostgreSQL restores previous session values.
3. **Cancellation & Rollback Safety:**
   - Transactions observe `context.Context` cancellation. If cancelled, operations abort immediately and uncommitted mutations are rolled back.
   - Child errors inside nested transactions roll back to PostgreSQL `SAVEPOINT`s without aborting the parent transaction block.
4. **Read-Only Enactment:**
   - When `ReadOnly: true`, the transaction executes `SET TRANSACTION READ ONLY`. Any mutation attempt immediately raises PostgreSQL error `25006: cannot execute INSERT/UPDATE/DELETE in a read-only transaction`.

---

## 3. Concurrency & Multi-Tenant Interleaving Guarantees

Under high-concurrency workloads where multiple workers share a finite PostgreSQL connection pool:
- Multiple goroutines executing `RunInScopedTransaction` with distinct `TenantID`s (e.g. `tenant_worker_0` .. `tenant_worker_19`) and claims run concurrently across pooled connections.
- Each connection checked out by worker $N$ observes strictly `ruxbase.tenant_id = tenant_worker_N`.
- Subsequent checkouts of the same physical connection by other workers observe zero residue from previous transactions (`current_setting('ruxbase.tenant_id', true)` is empty/null).
- The test suite in `core/tx_scope_test.go` verifies this invariant over 20 concurrent workers and 200 serialized transaction checkouts.

---

## 4. Verification Evidence

1. **Role and Tenant Isolation (`TestScopedTransactionRoleAndTenantIsolation`):**
   - Verified that inside transaction, `current_user` is `ruxbase_authenticated`, `ruxbase.token` is valid, `ruxbase.rule` is `list`, `ruxbase.tenant_id` is set, and claims are readable.
   - Verified that immediately upon commit, connection pool checkout reveals default application role and completely empty tenant/token settings.
2. **Rollback & Cancellation Isolation (`TestScopedTransactionRollbackAndCancellationIsolation`):**
   - Verified that uncommitted mutations are completely rolled back upon error, and connection settings are cleared.
   - Verified that pre-cancelled contexts reject execution immediately without leaking state.
3. **Privilege Elevation Protection (`TestScopedTransactionElevationProtection`):**
   - Verified that attempts to escalate to `postgres`, `admin`, or `superuser` are rejected with `unsafe role escalation`.
   - Verified that superusers cannot enter restricted RLS transaction scopes.
4. **Read-Only Enforcement (`TestScopedTransactionReadOnly`):**
   - Verified that write operations in read-only scopes trigger PostgreSQL error `25006`.
5. **High-Concurrency Abuse Matrix (`TestScopedTransactionConcurrentAbuse`):**
   - 20 concurrent goroutines executing 200 interleaved scoped transactions across the shared pool.
   - Proved 0 cross-tenant leakages and 0 residual settings on final pool checkout.
