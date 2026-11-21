# Wave 03: Enterprise IAM & Multi-Tenancy — Session Lifecycle & Claim Freshness Specification

```
Document ID:     RUXBASE-WAVE03-SESSION-LIFECYCLE
Version:         1.0.0
Milestone:       W03-02 (Session/Token Lifecycle, Revocation & Safe Claim Freshness)
Status:          ACCEPTED DESIGN SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Milestone W03-02 specifies the session and token lifecycle for Ruxbase Enterprise IAM. In distributed and multi-tenant environments, standard stateless JWTs pose significant security risks:
1. **Stale Role Escalation**: A user demoted from `admin` to `guest` retains administrative capabilities until their JWT expires.
2. **Delayed Revocation**: Banned users or compromised sessions cannot be instantly terminated across API and realtime streams.
3. **Client-Editable Claim Injection**: Clients injecting fake tenancy or role claims in request bodies or query parameters might trick naive policy evaluators.

Ruxbase solves these challenges by combining cryptographic JWT signing with **Authoritative Claim Freshness & Instant Revocation ($T=0$)**.

---

## 2. Token Architecture & Claims

```
                     Ruxbase Tenant JWT
  ┌────────────────────────────────────────────────────────┐
  │ Header: {"alg": "HS256", "typ": "JWT"}                 │
  ├────────────────────────────────────────────────────────┤
  │ Payload:                                               │
  │   "id":           "usr_alice01"                        │
  │   "type":         "auth"                               │
  │   "collectionId": "users"                              │
  │   "tokenKey":     "tk_live_random987"                  │
  │   "tenantId":     "tnt_acme001"                        │
  │   "exp":          1732204800                           │
  └────────────────────────────────────────────────────────┘
```

### 2.1 The Two-Tier Validation Pattern
When a client presents a Tenant JWT:
1. **Tier 1 — Cryptographic Verification**: Signature and `exp` timestamp are verified statelessly.
2. **Tier 2 — State & Freshness Verification**:
   - `tokenKey` is verified against the record's current `tokenKey` in the database. If revoked or rotated, the token is rejected immediately.
   - `tenantId` is verified for active status in `_tenants`.
   - The user's membership and current role are retrieved from `_tenant_memberships`. **The live database role always takes precedence over any claim embedded in the token payload.**

---

## 3. Revocation & Non-Tampering Invariants

1. **Instant Session Termination ($T=0$)**: Calling `RevokeUserSession(userId)` rotates the user's `tokenKey`, invalidating all active tokens across all tenants immediately without waiting for TTL expiry.
2. **Membership Demotion Zero-Staleness**: If an administrator demotes a user from `admin` to `member`, the very next HTTP request evaluates with `member` permissions, even if the client's cached JWT claims `admin`.
3. **No Client Claim Overrides**: Clients cannot specify or mutate authorization claims (`@request.auth.roles`, `@request.auth.tenantId`) via request bodies, URL queries, or custom HTTP headers.
