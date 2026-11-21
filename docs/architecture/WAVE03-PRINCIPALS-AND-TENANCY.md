# Wave 03: Enterprise IAM & Multi-Tenancy — Principals & Membership Specification

```
Document ID:     RUXBASE-WAVE03-PRINCIPALS-TENANCY
Version:         1.0.0
Milestone:       W03-01 (Specify Principals, Service Identity, Roles & Tenant Boundaries)
Status:          ACCEPTED DESIGN SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Wave 03 introduces first-class Enterprise Identity and Access Management (IAM) and multi-tenancy to Ruxbase without compromising the ergonomics or backward compatibility of PocketBase auth collections.

In traditional architectures, multi-tenancy is often added as an afterthought via ad-hoc SQL filters or application-level middleware. In Ruxbase, tenancy and identity are unified directly into the platform kernel:
1. **Unified Principal Model**: All identities (end-users from auth collections, automated service accounts, superusers, and anonymous visitors) conform to a single, immutable `Principal` contract.
2. **Explicit Tenancy Boundaries**: Every multi-tenant operation binds to a verified `Tenant` (organization/workspace). Missing, inactive, or unauthorized tenant contexts fail closed.
3. **Structured Memberships**: Users participate in tenants through explicit `TenantMembership` records bearing defined roles (`owner`, `admin`, `member`, `guest`) and fine-grained permissions.
4. **Service Principals**: Machine-to-machine integrations operate under distinct service identities governed by least-privilege token scopes, completely decoupled from human user credentials.

---

## 2. Principal Domain Model

```
                          ┌──────────────────────────┐
                          │        Principal         │
                          ├──────────────────────────┤
                          │ ID                       │
                          │ Type (User|Service|Anon) │
                          │ TenantID                 │
                          │ Roles[]                  │
                          │ Permissions[]            │
                          └─────────────┬────────────┘
                                        │
                 ┌──────────────────────┴──────────────────────┐
                 ▼                                             ▼
     ┌───────────────────────┐                     ┌───────────────────────┐
     │    User Principal     │                     │   Service Principal   │
     ├───────────────────────┤                     ├───────────────────────┤
     │ AuthCollection: users │                     │ ServiceID: srv_1234   │
     │ Email: user@org.com   │                     │ Scopes: [read, sync]  │
     │ Memberships[]         │                     │ AllowedTenants[]      │
     └───────────────────────┘                     └───────────────────────┘
```

### 2.1 Principal Types
- `PrincipalTypeSuperuser`: Full administrative access to the Ruxbase system catalogs, schema DDL, and cross-tenant maintenance.
- `PrincipalTypeUser`: An authenticated human end-user whose record originates in a Ruxbase auth collection (e.g. `users`).
- `PrincipalTypeService`: A machine or backend service accessing APIs via dedicated service credentials with explicit permission scopes.
- `PrincipalTypeAnonymous`: An unauthenticated caller bound strictly to public collections and public RPC routines.

### 2.2 Tenant Model
A Tenant represents an isolated administrative or organizational domain:
- `ID`: Unique 15-character alphanumeric identifier.
- `Name`: Human-readable display name (e.g. "Acme Corp").
- `Slug`: URL-safe identifier unique across active tenants.
- `Status`: `active`, `suspended`, or `archived`. Suspended or archived tenants immediately deny all operational requests.

### 2.3 Membership Model
A User interacts with a Tenant exclusively through a `TenantMembership`:
- `TenantID`: Foreign key to `_tenants.id`.
- `UserID`: Foreign key to the user record in the auth collection.
- `Role`: Standardized role identifier (`owner`, `admin`, `member`, `guest`).
- `Status`: `active`, `invited`, or `revoked`.

---

## 3. Kernel Authority & Fail-Closed Invariants

1. **No Implicit Tenancy**: A request attempting to access tenant-scoped resources without providing an explicit, valid tenant identifier fails closed with `403 Forbidden` (`err_tenant_context_required`).
2. **Membership Verification**: Possession of a valid user JWT does not grant access to a tenant unless an `active` membership exists for that specific `(tenant_id, user_id)` tuple.
3. **Tenant Lifecycle Revocation**: Suspending or archiving a tenant immediately blocks all subsequent operations across REST, SQL, Realtime, Storage, and RPC without awaiting token expiration.
4. **Immutable Identity Propagation**: Principal attributes (ID, TenantID, Roles) are immutable within the execution context of a request event and cannot be overwritten by client-supplied payload parameters.
