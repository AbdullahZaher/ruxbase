# Ruxbase Wave 02 — Reviewed Indexes, Partitioning & PostgreSQL Extensions

**Document Reference:** `docs/architecture/WAVE02-INDEXES-AND-PARTITIONING.md`  
**Milestone:** W02-05 / P1 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Architectural Mission & Invariants

PostgreSQL provides sophisticated storage structures and extension ecosystems. Wave 02 ensures:
1. **Reviewed Indexes:** Safe creation, introspection, and maintenance of modern index types:
   - **B-Tree (`btree`):** Default for equality, range queries (`<`, `<=`, `=`, `>=`, `>`), sorting (`ORDER BY`), and unique constraints.
   - **GIN (`gin`):** Generalized Inverted Index for JSONB containment (`@>`), full-text search (`tsvector`), and multi-component text search (`pg_trgm`).
   - **GiST (`gist`):** Generalized Search Tree for geometric shapes (`point`, `box`), multidimensional nearest-neighbor, and exclusion constraints.
   - **HNSW (`hnsw`) / IVFFlat (`ivfflat`):** Approximate Nearest Neighbor index for vector embeddings (`pgvector`) with cosine, L2, or inner product distance metrics.
   - **Partial & Expression Indexes:** Safe predicate enforcement (`WHERE is_active = true`) without raw SQL injection vulnerabilities.
2. **Approved PostgreSQL Extensions Whitelist:**
   - Extensions run with elevated privileges inside the database cluster.
   - Ruxbase enforces a strict allowlist of battle-tested extensions: `vector`, `pg_trgm`, `btree_gin`, `btree_gist`, `uuid-ossp`, `pgcrypto`, `unaccent`, and `fuzzystrmatch`.
   - Untrusted or dangerous extensions (e.g. untrusted procedural languages or filesystem access plugins) are rejected unconditionally.
3. **Declarative Partitioning Boundaries:**
   - Full catalog introspection of partitioned tables (`relkind = 'p'`), partitioning strategies (`RANGE`, `LIST`, `HASH`), partition key columns, and child partition hierarchies (`pg_inherits`).
   - Unified collection abstraction: parent partitioned tables can be exposed as Collections, allowing transparent partition routing on write and partition pruning on read.

---

## 2. PostgreSQL Extensions Catalog & Governance

| Extension | Primary Use Case in Ruxbase | Approved Policy |
|---|---|---|
| **`vector`** | pgvector high-dimensional embeddings and semantic search | Allowed |
| **`pg_trgm`** | Trigram text similarity, regex indexing, and fast `ILIKE` | Allowed |
| **`btree_gin`** | Composite GIN indexes combining scalar columns with JSONB | Allowed |
| **`btree_gist`** | Composite GiST indexes and temporal exclusion constraints | Allowed |
| **`uuid-ossp`** | Standard UUID generation functions | Allowed |
| **`pgcrypto`** | Cryptographic hashing (SHA-256, HMAC) and random generators | Allowed |
| **`unaccent`** | Accent-insensitive search dictionaries | Allowed |
| **`fuzzystrmatch`** | Phonetic string comparisons (Levenshtein, Soundex) | Allowed |
| *Others* | Any unapproved extension | **DENY BY DEFAULT** |

---

## 3. Declarative Partitioning Specification

### 3.1 Catalog Representation
In PostgreSQL:
- Partitioned tables have `c.relkind = 'p'` in `pg_class`.
- Strategy and keys are defined in `pg_partitioned_table`:
  - `partstrat = 'r'`: Range partitioning (e.g., date-based partitions `FOR VALUES FROM ('2026-01-01') TO ('2026-02-01')`).
  - `partstrat = 'l'`: List partitioning (e.g., tenant-based partitions `FOR VALUES IN ('tenant_a', 'tenant_b')`).
  - `partstrat = 'h'`: Hash partitioning for distributed sharding.
- Child partition tables inherit from the partitioned parent and are indexed in `pg_inherits`.

### 3.2 Transparent Record Operations
When a partitioned table is registered as an exposed Collection:
- The parent partitioned table name is stored in the Collection definition.
- Writes (`INSERT INTO "partitioned_table" ...`) automatically route to the corresponding child partition based on the partition key value.
- Queries (`SELECT * FROM "partitioned_table" WHERE ...`) automatically utilize partition pruning, scanning only the relevant leaf partition.
