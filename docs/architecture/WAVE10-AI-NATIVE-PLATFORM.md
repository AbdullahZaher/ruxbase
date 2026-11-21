# Wave 10: AI-Native Platform & Plugin Ecosystem

**Document:** `docs/architecture/WAVE10-AI-NATIVE-PLATFORM.md`  
**Target Wave:** Wave 10 — AI-Native Platform & Plugin Ecosystem  

---

## 1. Opt-In MCP & Policy-Conforming Tool Discovery (W10-01)

Ruxbase preserves opt-in MCP server execution (`/mcp`, executing REST dispatch under the caller's token via `apis/mcp_api.go`) while adding scoped AI agent collection discovery (`core.DiscoverAgentTools` / `GET /api/ai/agents/{id}/tools`):
- System/privileged collections (`System == true` or `_`-prefixed names such as `_superusers`) are unconditionally filtered out of agent tool discovery.
- Collections outside an agent's `AllowedCollections` allowlist are never advertised.

---

## 2. Scoped AI Agent Identity & Least-Privilege Auditing (W10-02)

Every AI agent identity (`_ai_agents`) carries explicit `owner_id`, `scopes` (`collections:read`, `collections:write`, `collections:delete`), and `allowed_collections`:
- Cross-tenant access attempts (`agent.OwnerId != caller.Id`) are rejected cleanly with HTTP `403`.
- Every evaluation decision (`allow`, `deny`, `require_approval`) is written to `_ai_audit_logs`.

---

## 3. Human Approval & Action Classification (W10-03)

Tool operations are classified into `read`, `write`, `sensitive`, and `destructive`:
- `sensitive` and `destructive` actions strictly reject execution (`ErrAgentApprovalRequired`) unless accompanied by a matching `approvalId` approved by a superuser (`_ai_tool_approvals`).
- Approvals are bound to exact `(id, agent_id, tool_name, target_resource)` tuples and consumed atomically in PostgreSQL (`SET status = 'executed' WHERE status = 'approved'`), preventing replay and confused-deputy attacks.

---

## 4. Optional Knowledge/RAG & AI Gateway Boundaries (W10-04)

`_ai_knowledge_chunks` stores tenant-isolated (`owner_id` + `namespace`) chunks queryable via `/api/ai/knowledge/search`. Cross-tenant knowledge search queries are strictly impossible because `owner_id` is bound from verified `e.Auth.Id`. External AI model/embedding gateways remain strictly optional.

---

## 5. Versioned Plugin SDK & Lifecycle Isolation (W10-05)

`core/plugin_sdk.go` provides `_plugins` lifecycle persistence (`LoadPlugin`, `UpgradePlugin`, `UnloadPlugin`):
- **Semver Compatibility Gate:** Enforces `min_core_version <= CorePluginSDKVersion` (`1.0.0`).
- **Isolation Guard:** Rejects any plugin requesting prohibited capabilities (`host_secrets`, `superuser_db`, `bypass_rls`, `os_exec`).
