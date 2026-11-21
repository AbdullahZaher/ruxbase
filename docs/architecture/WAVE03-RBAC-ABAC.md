# Wave 03: Enterprise IAM & Multi-Tenancy — Scalable RBAC/ABAC Evaluation

```
Document ID:     RUXBASE-WAVE03-RBAC-ABAC
Version:         1.0.0
Milestone:       W03-03 (Scalable RBAC/ABAC Evaluation with Explicit Role & Permission Defaults)
Status:          ACCEPTED DESIGN SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Milestone W03-03 establishes a unified, high-performance Role-Based (RBAC) and Attribute-Based (ABAC) access control evaluation engine.

Enterprise deployments require both:
- **Coarse-grained RBAC**: Rapid checks based on organizational titles and roles (`owner`, `admin`, `member`, `guest`, or custom tenant roles).
- **Fine-grained ABAC**: Contextual checks examining resource ownership (e.g. `resource.created_by == principal.id`), tenant boundary matching, and request environment.

---

## 2. Permission Model & System Roles

### 2.1 Standard Permissions Catalog
```
  records.read
  records.create
  records.update
  records.delete
  files.upload
  files.delete
  members.manage
  settings.manage
  * (Superuser / Wildcard)
```

### 2.2 Default Role Mappings
| Role | Assigned Permissions |
|---|---|
| `owner` | `*` (Full authority within tenant) |
| `admin` | `records.*`, `files.*`, `members.manage` |
| `member` | `records.read`, `records.create`, `records.update`, `files.upload` |
| `guest` | `records.read` |

---

## 3. Policy Evaluation Algorithm

When `EvaluateAccess(principal, resource, action)` is executed:
1. **Superuser Bypass**: If `principal.Type() == PrincipalTypeSuperuser`, allow immediately.
2. **Tenant Boundary Assertion**: If `resource.TenantID != ""` and `principal.TenantID != resource.TenantID`, deny immediately (`403 Forbidden`).
3. **RBAC Permission Check**: Expand all permissions belonging to the principal's active roles in the tenant. If any role grants the target action (or wildcard `*`), grant access.
4. **ABAC Attribute Evaluation**: If RBAC does not grant global action access, evaluate attribute rules:
   - E.g. If action is `records.update` and `resource.CreatedBy == principal.ID`, allow under self-ownership policy.
5. **Deny by Default**: If no rule grants access, fail closed with denial.
