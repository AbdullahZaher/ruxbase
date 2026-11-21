# Wave 04: Unified Data API & Developer Experience — Optional GraphQL Contract Architecture

```
Document ID:     RUXBASE-WAVE04-GRAPHQL-CONTRACT
Version:         1.0.0
Milestone:       W04-07 (Optional GraphQL API sharing the same policy and transaction contracts)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Milestone W04-07 defines the architectural contract and execution model for an optional GraphQL interface in Ruxbase.
In alignment with the core principle **"One authorization and transaction engine across all interfaces"**, GraphQL does not introduce an independent permission model or bypass database-level RLS policies.

Key architectural requirements:
1. **Identical Authorization Engine**: Every GraphQL query and mutation executes within a PostgreSQL `RunInScopedTransaction` bound to the caller's active `Principal`, role, and `tenant_id`.
2. **Zero Core Binary Bloat**: Kept strictly optional, implemented via a pluggable module or separate extension without forcing heavy GraphQL dependencies into the core monolith.
3. **N+1 Query Elimination**: Field resolvers batch nested relational queries into composite PostgreSQL CTEs (`WITH` clauses and `json_agg`), preventing database round-trip amplification.
4. **Strict Complexity and Depth Limits**: Rejects unbounded queries, circular relation traversals, and excessive pagination requests ($N \le 500$).

---

## 2. Schema Derivation & Policy Mapping

The GraphQL schema dynamically mirrors the Ruxbase collection and exposed native table catalog:

```graphql
type Query {
  # Collection records query
  records(collection: String!, filter: String, sort: String, page: Int, perPage: Int): RecordConnection!
  record(collection: String!, id: ID!): Record

  # Exposed native table query (governed by table exposure rules)
  table(name: String!, filter: String, sort: String, page: Int, perPage: Int): TableConnection!
}

type Mutation {
  createRecord(collection: String!, data: JSON!): Record!
  updateRecord(collection: String!, id: ID!, data: JSON!): Record!
  deleteRecord(collection: String!, id: ID!): Boolean!
  bulkInsert(table: String!, rows: [JSON!]!): BulkResult!
}
```

### Execution Flow in Scoped Transaction

```
Client GraphQL Request
         │
         ▼
[Authentication & Principal Extraction]
         │
         ▼
[Query Complexity & Depth Budget Check]
         │
         ▼
[RunInScopedTransaction]
   ├── SET LOCAL role = 'ruxbase_authenticated'
   ├── SET LOCAL ruxbase.tenant_id = :tenantId
   ├── SET LOCAL ruxbase.token = :scopedToken
   ├── SET LOCAL ruxbase.auth_rule = :rule
   │
   └── [PostgreSQL Evaluates Native RLS Policies]
         │
         ▼
JSON Response Envelope
```

---

## 3. Security Boundary & Failure Semantics

- **Deny-by-Default**: Tables and collections without explicit public or role-based read rules return `null` or GraphQL field errors (`FORBIDDEN`).
- **Tenant Isolation**: Unscoped queries automatically filter rows matching `tenant_id = current_setting('ruxbase.tenant_id')`.
- **Audit Logging**: Graph operations inherit standard transaction timeouts (`statement_timeout = 15s`, `lock_timeout = 3s`).
