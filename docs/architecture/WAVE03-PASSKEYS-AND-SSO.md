# Wave 03: Enterprise IAM & Multi-Tenancy — Passkeys & Enterprise SSO Architecture

```
Document ID:     RUXBASE-WAVE03-PASSKEYS-AND-SSO
Version:         1.0.0
Milestone:       W03-07 (Passkeys and Enterprise SSO as Separately Approved Optional Capabilities)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION & EXTENSION CONTRACT
```

---

## 1. Executive Summary & Design Scope

Milestone W03-07 defines the enterprise extension architecture for Passkeys (WebAuthn / FIDO2) and Enterprise SSO (SAML 2.0 / OpenID Connect).

In alignment with the Ruxbase Architectural Contract:
- **Zero Bloat in Core**: Ruxbase remains lightweight and self-contained; optional enterprise identity protocols do not burden core deployments with mandatory external services or heavyweight XML cryptography packages.
- **Pluggable Extension Interface**: WebAuthn and SSO integrate cleanly through documented authentication provider contracts and hook lifecycles.
- **Tenant-Bound Identity Providers**: Enterprise SSO configurations are strictly partitioned per tenant; identity assertions from Company A's IdP cannot authenticate users into Company B's tenant.

---

## 2. Pluggable Identity Provider Contract

```go
// IdentityProvider defines the extension interface for advanced authentication plugins.
type IdentityProvider interface {
    // ProviderName returns the identifier of the auth provider (e.g. "webauthn", "saml", "oidc").
    ProviderName() string

    // Authenticate processes an incoming assertion or challenge response.
    Authenticate(ctx context.Context, payload []byte) (*ExternalAuthResult, error)

    // MapPrincipal resolves or provisions an internal UserPrincipal mapped to the authorized tenant.
    MapPrincipal(ctx context.Context, app App, tenantId string, authResult *ExternalAuthResult) (*UserPrincipal, error)
}

type ExternalAuthResult struct {
    Subject       string            // Unique external user identifier
    Email         string            // Verified email address
    DisplayName   string            // User full name
    Groups        []string          // IdP directory groups
    RawAttributes map[string]string // Custom directory attributes
}
```

---

## 3. Physical Schema Extensions (Pluggable)

### 3.1 Passkey Credentials (`_user_passkeys`)
When the WebAuthn extension is active:
```sql
CREATE TABLE IF NOT EXISTS _user_passkeys (
    id VARCHAR(15) PRIMARY KEY,
    user_id VARCHAR(15) NOT NULL,
    credential_id TEXT NOT NULL UNIQUE,
    public_key BYTEA NOT NULL,
    attestation_type VARCHAR(32) NOT NULL DEFAULT 'none',
    sign_count BIGINT NOT NULL DEFAULT 0,
    transports JSONB NOT NULL DEFAULT '[]'::jsonb,
    backup_eligible BOOLEAN NOT NULL DEFAULT false,
    backup_state BOOLEAN NOT NULL DEFAULT false,
    created TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx__user_passkeys_user ON _user_passkeys (user_id);
```

### 3.2 Tenant SSO Configurations (`_tenant_sso_configs`)
```sql
CREATE TABLE IF NOT EXISTS _tenant_sso_configs (
    id VARCHAR(15) PRIMARY KEY,
    tenant_id VARCHAR(15) NOT NULL REFERENCES _tenants(id) ON DELETE CASCADE,
    provider_type VARCHAR(32) NOT NULL, -- "saml", "oidc", "google", "microsoft", "okta"
    domain VARCHAR(255) NOT NULL UNIQUE, -- e.g. "acme.com"
    issuer_url TEXT NOT NULL,
    client_id VARCHAR(255) NOT NULL,
    client_secret_hash VARCHAR(255) NOT NULL DEFAULT '',
    metadata_xml TEXT,
    attribute_mapping JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT true,
    created TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_tenant_sso_provider UNIQUE (tenant_id, provider_type)
);
CREATE INDEX IF NOT EXISTS idx__tenant_sso_domain ON _tenant_sso_configs (domain);
```

---

## 4. JIT Provisioning & Role Mapping Security Guarantees

1. **Domain Verification**: An SSO configuration can only be claimed for domains owned by the tenant.
2. **Deterministic Role Mapping**: Directory groups from the IdP (e.g. `Engineering-Lead`) map to Ruxbase tenant roles (`admin`, `member`, or custom roles) via explicit attribute rules. If no mapping matches, the user defaults to `guest` or is rejected.
3. **Tenant Boundary Lockdown**: An SSO response signed by Tenant 1's IdP is rejected if presented to Tenant 2.
