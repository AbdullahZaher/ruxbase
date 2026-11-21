# Wave 03: Enterprise IAM & Multi-Tenancy — Negative Cross-Tenant Isolation Matrix

```
Document ID:     RUXBASE-WAVE03-NEGATIVE-ISOLATION-MATRIX
Version:         1.0.0
Milestone:       W03-05 (Negative Cross-Tenant Tests for APIs, Vector Search, Realtime, Jobs, Backups, Plugins and MCP)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Isolation Scope

Milestone W03-05 provides exhaustive negative test verification across seven critical platform vectors:
1. **REST APIs**: Fail-closed record CRUD and listing across tenant boundaries.
2. **Vector Search & Embeddings**: Strict tenant filtering in similarity queries.
3. **Realtime Notifications**: Zero cross-tenant event leakage on pub/sub streams.
4. **Jobs & Workflows**: Tenant isolation in background worker queues and execution scopes.
5. **Backups & Restores**: Strict tenant partition dumps and replay isolation.
6. **Plugins & Server Extensions**: Context boundaries for user-defined hooks.
7. **MCP Tools & AI Metadata**: Agentic boundaries preventing cross-tenant context access.

---

## 2. Negative Isolation Test Matrix

| Vector | Attack / Cross-Tenant Scenario | Expected Outcome | Failure Condition |
|---|---|---|---|
| **APIs** | Tenant A token requests record from Tenant B by explicit ID | `404 Not Found` or `403 Forbidden` | Row data exposed |
| **Vector Search** | Vector similarity query in Tenant A with embeddings identical to Tenant B record | Only Tenant A records returned; Tenant B filtered out | Tenant B vector returned |
| **Realtime** | Tenant A listener connected to SSE topic for shared collection | Only receives events matching Tenant A | Receives Tenant B event |
| **Jobs** | Tenant A triggers background job modifying records | Job execution scoped strictly to Tenant A; zero rows affected in Tenant B | Tenant B row modified |
| **Backups** | Tenant A backup export triggered | Export archive contains only records with `tenant_id == Tenant A` | Tenant B records exported |
| **Plugins** | Hook running on Tenant A mutation accesses app database | Scoped execution cannot query or manipulate Tenant B entities | Cross-tenant DB access |
| **MCP** | Model Tool query with Tenant A identity attempts cross-tenant introspection | Introspection query constrained to Tenant A scope | Tenant B metadata exposed |

---

## 3. Strict Fail-Closed Invariants

1. **Absent Context Invariant**: Any query or request under restricted application roles lacking an explicit `ruxbase.tenant_id` setting must return 0 rows.
2. **Deterministic Information Hiding**: Requests attempting to view resources of other tenants must fail with `404 Not Found` or generic access denial to prevent enumeration attacks.
3. **Storage Boundary Enforcement**: File downloads for paths outside the authenticated tenant prefix are rejected with `403 Forbidden`.
