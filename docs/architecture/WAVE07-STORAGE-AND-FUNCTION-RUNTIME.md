# Wave 07: Storage & Function Runtime Architecture

## 1. Executive Mission & Objective

Wave 07 delivers unified, PostgreSQL-native storage bucket management and safely isolated function compute execution for Ruxbase. It provides self-hosted Supabase parity for storage and edge compute without sacrificing Ruxbase's lightweight, single-binary architecture.

---

## 2. Storage Subsystem & Authorization Architecture

### 2.1 Schema & Data Model

Storage metadata is tracked natively in PostgreSQL:

```sql
-- Storage buckets
CREATE TABLE IF NOT EXISTS _storage_buckets (
    id            TEXT PRIMARY KEY NOT NULL,
    name          TEXT UNIQUE NOT NULL,
    public        BOOLEAN DEFAULT false NOT NULL,
    file_size_limit BIGINT DEFAULT 10485760 NOT NULL, -- 10MB default
    allowed_mime_types JSONB,
    created       TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated       TIMESTAMPTZ DEFAULT now() NOT NULL
);

-- Storage objects
CREATE TABLE IF NOT EXISTS _storage_objects (
    id            TEXT PRIMARY KEY NOT NULL,
    bucket_id     TEXT NOT NULL REFERENCES _storage_buckets(id) ON DELETE CASCADE,
    name          TEXT NOT NULL,
    owner_id      TEXT DEFAULT '' NOT NULL,
    size          BIGINT DEFAULT 0 NOT NULL,
    mime_type     TEXT DEFAULT 'application/octet-stream' NOT NULL,
    etag          TEXT DEFAULT '' NOT NULL,
    metadata      JSONB,
    created       TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated       TIMESTAMPTZ DEFAULT now() NOT NULL,
    UNIQUE(bucket_id, name)
);
CREATE INDEX IF NOT EXISTS idx__storage_objects_bucket_owner ON _storage_objects (bucket_id, owner_id);
```

### 2.2 Security & Authorization Boundaries
1. **Public vs Private Buckets**: Public buckets permit anonymous read access; private buckets enforce caller identity checks or signed download URLs.
2. **Owner-Scoped Object Isolation**: Cross-tenant and cross-user modifications are rejected unless administrative bypass or bucket policy explicitly authorizes it.
3. **Signed Access URLs**: HMAC-SHA256 signed download tokens allow time-limited (TTL) access to private objects without disclosing persistent credentials.
4. **Quota & MIME Enforcement**: Uploads exceeding bucket `file_size_limit` or violating `allowed_mime_types` are rejected before writing to storage backend.

---

## 3. Function Runtime Subsystem & Secrets Isolation

### 3.1 Schema & Deployment Model

```sql
CREATE TABLE IF NOT EXISTS _functions (
    id            TEXT PRIMARY KEY NOT NULL,
    name          TEXT UNIQUE NOT NULL,
    entrypoint    TEXT NOT NULL,
    runtime       TEXT DEFAULT 'js' NOT NULL, -- js, wasm, go-plugin
    code          TEXT NOT NULL,
    env_vars      JSONB,                      -- encrypted / isolated function secrets
    timeout_sec   INTEGER DEFAULT 10 NOT NULL,
    status        TEXT DEFAULT 'active' NOT NULL,
    version       INTEGER DEFAULT 1 NOT NULL,
    created       TIMESTAMPTZ DEFAULT now() NOT NULL,
    updated       TIMESTAMPTZ DEFAULT now() NOT NULL
);
```

### 3.2 Secrets Boundary & Host Isolation
- **No Host Credential Leakage**: Function instances run in an isolated execution context. Sensitive application database credentials, superuser secrets, and internal host parameters are strictly stripped and never exposed to function execution environments.
- **Execution Timeouts**: Enforced per-invocation deadlines prevent runaway scripts or denial-of-service through infinite loops.
- **Trusted vs Untrusted Separation**: Core Go/JS application hooks remain trusted and execute directly within the engine; user-deployed compute functions run inside the sandboxed runtime with restricted capabilities.
