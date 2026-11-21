# Wave 03: Enterprise IAM & Multi-Tenancy — Service Accounts & Secrets Lifecycle

```
Document ID:     RUXBASE-WAVE03-SERVICE-ACCOUNTS-AND-SECRETS
Version:         1.0.0
Milestone:       W03-06 (Least-Privilege Service Accounts, Secrets Lifecycle and Scoped Operator Access)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Architecture Scope

Milestone W03-06 establishes a least-privilege, automated machine-to-machine (M2M) service account subsystem.

Unlike human user credentials, service account API keys:
- Are issued with immutable prefixes (`rx_svc_<prefix>_...`) for safe identification and indexing.
- Are stored exclusively as cryptographic hashes (`SHA-256`), never plaintext.
- Enforce explicit scope boundaries (e.g. `["records.read"]`) preventing privilege escalation.
- Enforce tenant isolation via `allowed_tenants` or dedicated tenant binding.
- Support automated rotation, instant revocation, and TTL expiration.

---

## 2. Service Account Data Model

Physical system table: `_service_accounts`
```sql
CREATE TABLE IF NOT EXISTS _service_accounts (
    id VARCHAR(15) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    tenant_id VARCHAR(15) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    scopes JSONB NOT NULL DEFAULT '[]'::jsonb,
    allowed_tenants JSONB NOT NULL DEFAULT '[]'::jsonb,
    api_key_hash VARCHAR(255) NOT NULL,
    api_key_prefix VARCHAR(16) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    created TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
```

---

## 3. Key Lifecycle & Constant-Time Verification

1. **Generation**:
   - Plaintext format: `rx_svc_<prefix:8>_<secret:32>`
   - Hash stored: `SHA-256(plaintext)`
2. **Verification**:
   - Extract `<prefix:8>`. Look up record in index.
   - Compute `SHA-256(incoming_key)`.
   - Compare stored hash with incoming hash using `subtle.ConstantTimeCompare`.
3. **Revocation & Rotation**:
   - Rotating an API key immediately generates a new secret, updates the hash, and invalidates the previous key.
   - Setting status to `revoked` instantly invalidates the service account at $T=0$.
