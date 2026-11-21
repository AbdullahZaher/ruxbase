# Ruxbase Wave 02 — PostgreSQL Native Engine Schema Mapping & Type Inventory

**Document Reference:** `docs/architecture/WAVE02-SCHEMA-MAPPING.md`  
**Milestone:** W02-01 / P0 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Architectural Mission & Invariants

Wave 02 transitions Ruxbase into a dual-mode platform:
1. **Collections-First Experience:** Full backwards-compatible developer ergonomics, client SDK contracts, and dynamic schema definitions inherited from PocketBase.
2. **SQL-First Native Engine:** Authentic PostgreSQL tables, namespaces, constraints, typed columns, foreign keys, and views that can be queried, migrated, and managed directly using standard PostgreSQL tools.

### Non-Negotiable Invariants
- **Unified Policy Kernel:** Native SQL tables and Collections share the **exact same** identity, transaction scope, and Row-Level Security (RLS) enforcement kernel established and certified in Wave 01.
- **Deny-by-Default Exposure:** A table in the PostgreSQL database is **never** exposed to APIs, SDKs, or clients simply because it exists. Exposure requires explicit, authenticated, and authorized declaration.
- **Zero Superuser at Runtime:** All schema introspection, metadata extraction, and table management execute under the restricted `ruxbase_app` role using PostgreSQL catalog views (`information_schema`, `pg_catalog`).
- **PostgreSQL 17 & 18 Parity:** No SQLite syntax, no SQLite pragmas, and no PostgreSQL 18-only syntax without backward compatibility with PostgreSQL 17.

---

## 2. Collection Field to PostgreSQL Column Mapping Matrix

The table below catalogs all 16 built-in Ruxbase collection field types, their native PostgreSQL column definitions, default values, storage formats, and indexing strategies:

| Field Type (`core.FieldType*`) | PostgreSQL Column Type (`field.ColumnType(app)`) | Storage Semantics | Nullability & Default | Recommended Index Type |
|---|---|---|---|---|
| **`autodate`** | `TIMESTAMPTZ DEFAULT NULL` | 8-byte timestamp with timezone | Nullable; managed by application lifecycle | B-Tree |
| **`bool`** | `BOOLEAN DEFAULT FALSE NOT NULL` | 1-byte boolean | Not null; false default | B-Tree / Partial |
| **`date`** | `TIMESTAMPTZ DEFAULT NULL` | 8-byte timestamp with timezone | Nullable; ISO 8601 string in API | B-Tree |
| **`editor`** | `TEXT DEFAULT '' NOT NULL` | Variable-length text | Not null; empty string default | GIN / GiST (full-text) |
| **`email`** | `TEXT DEFAULT '' NOT NULL` | Case-insensitive text | Not null; validated via regex in app | B-Tree / Unique Lower |
| **`file` (single)** | `TEXT DEFAULT '' NOT NULL` | File identifier / storage path | Not null; empty string default | B-Tree |
| **`file` (multiple)** | `JSONB DEFAULT '[]' NOT NULL` | JSON array of file paths | Not null; array default | GIN (jsonb_path_ops) |
| **`geoPoint`** | `POINT` / `JSONB DEFAULT '{}' NOT NULL` | 16-byte point (x,y) or GeoJSON | Coordinates (lng, lat) | GiST (PostGIS/Spatial) |
| **`json`** | `JSONB DEFAULT 'null' NOT NULL` | Binary JSON | Not null; JSONB parsed | GIN (jsonb_path_ops) |
| **`number`** | `DOUBLE PRECISION DEFAULT 0 NOT NULL` | 8-byte floating point | Not null; 0 default | B-Tree |
| **`number` (exact/decimal)** | `NUMERIC(p,s) DEFAULT 0 NOT NULL` | Arbitrary precision decimal | For currency, finance, precise accounting | B-Tree |
| **`password`** | `TEXT DEFAULT '' NOT NULL` | Argon2 / bcrypt hash string | Not null; hidden from API serialize | None |
| **`relation` (single)** | `TEXT DEFAULT '' NOT NULL` | Referenced record ID | Optional FK: `REFERENCES target(id)` | B-Tree |
| **`relation` (multiple)** | `JSONB DEFAULT '[]' NOT NULL` | JSON array of record IDs | Array of string IDs | GIN (jsonb_path_ops) |
| **`search`** | `TSVECTOR` (Generated Always) | Lexical token search vector | Generated column stored | GIN (to_tsvector) |
| **`select` (single)** | `TEXT DEFAULT '' NOT NULL` | Selected choice string | Not null; enumerated options | B-Tree |
| **`select` (multiple)** | `JSONB DEFAULT '[]' NOT NULL` | JSON array of chosen strings | Not null; array default | GIN (jsonb_path_ops) |
| **`text`** | `TEXT DEFAULT '' NOT NULL` | Variable-length string | Not null; empty string default | B-Tree |
| **`url`** | `TEXT DEFAULT '' NOT NULL` | URL string | Not null; format validated | B-Tree |
| **`vector`** | `VECTOR(dimensions)` via `pgvector` | Dense float array | Length matches configured dimension | HNSW / IVFFlat |

---

## 3. Reverse-Mapping Matrix: Native PostgreSQL to Collection Field

When introspecting native PostgreSQL tables created outside the Collections UI (e.g., via SQL migrations, external ORMs, or direct DB administrative tools), column definitions are reverse-mapped into Collection fields as follows:

| PostgreSQL Data Type (`udt_name` / `data_type`) | Inferred Collection Field Type | Target Field Config / Constraints |
|---|---|---|
| `boolean`, `bool` | `FieldTypeBool` | Default value mapped to boolean |
| `smallint`, `int2`, `integer`, `int4`, `bigint`, `int8` | `FieldTypeNumber` | Integer numbers (no decimals) |
| `numeric`, `decimal`, `real`, `float4`, `float8`, `double precision` | `FieldTypeNumber` | Floating point or exact numeric |
| `text`, `varchar`, `character varying`, `bpchar`, `char` | `FieldTypeText` | Text field (with Max length constraint if specified) |
| `timestamp with time zone`, `timestamptz` | `FieldTypeDate` | Date field with timezone support |
| `timestamp without time zone`, `timestamp` | `FieldTypeDate` | Parsed and serialized as UTC ISO 8601 |
| `date`, `time`, `time with time zone` | `FieldTypeText` or `FieldTypeDate` | Preserved as ISO string format |
| `jsonb`, `json` | `FieldTypeJSON` | Native JSON validation and query support |
| `uuid` | `FieldTypeText` | Regex validation: `^[0-9a-f-]{36}$` |
| `vector` (from `pgvector`) | `FieldTypeVector` | Dimensions extracted from type modifier |
| `point`, `geometry`, `geography` | `FieldTypeGeoPoint` | Geo coordinates |
| *Other / Custom Domain Types* | `FieldTypeJSON` or `FieldTypeText` | Fallback preserving data without corruption |

---

## 4. Primary Key & Identifier Architecture

### 4.1 Collection Default Primary Keys
Standard Collections use a 15-character alphanumeric ID (`TEXT PRIMARY KEY`), generated cryptographically by `security.RandomString(15)`.

### 4.2 Native PostgreSQL Primary Key Archetypes
Native tables support diverse primary key structures:
1. **UUID Keys:** `UUID PRIMARY KEY DEFAULT gen_random_uuid()` (PostgreSQL 13+ standard, fully supported in PG17/18).
2. **Serial / Sequence Keys:** `BIGSERIAL PRIMARY KEY` or `BIGINT GENERATED ALWAYS AS IDENTITY`.
3. **Natural Text Keys:** `TEXT PRIMARY KEY` (e.g. ISO country codes, slug identifiers).
4. **Composite Primary Keys:** `PRIMARY KEY (tenant_id, id)` or `PRIMARY KEY (user_id, role_id)`.

### 4.3 Introspection & Record Identification Guarantees
- Single-column primary keys are mapped directly to `record.Id`.
- For tables without a single `id` column, the primary key column name and type are tracked in the collection metadata options.
- Composite primary keys are represented as composite compound identifiers (`col1:col2`) for REST routing while executing parameterized composite equality in SQL.

---

## 5. Relational Constraints & Foreign Key Integrity

### 5.1 Native Foreign Key Mapping
Unlike the PocketBase default of loose string matching without foreign key constraints, Ruxbase Native Engine supports real database foreign keys:
```sql
ALTER TABLE "orders"
  ADD CONSTRAINT "fk_orders_customer"
  FOREIGN KEY ("customer_id")
  REFERENCES "customers" ("id")
  ON DELETE RESTRICT
  ON UPDATE CASCADE;
```

### 5.2 Supported Referential Actions
1. **`CASCADE`**: Deleting the parent automatically deletes child rows (managed transactionally by PostgreSQL engine).
2. **`SET NULL`**: Deleting the parent sets the foreign key column to NULL (requires column to be nullable).
3. **`RESTRICT` / `NO ACTION`**: Prevents parent deletion if dependent child records exist (fails with SQLSTATE 23503).

### 5.3 Junction Tables vs Multiple Relations
- Collections support multi-value relations via `JSONB` array of IDs (e.g. `["id1", "id2"]`).
- Native Engine supports standard normalized junction tables (`m2m_orders_tags` with `order_id` and `tag_id`), introspecting them into bidirectional virtual relation fields.

---

## 6. Constraints & Schema Validation

| Constraint Type | PostgreSQL Engine Implementation | Collection Model Representation |
|---|---|---|
| **NOT NULL** | `attnotnull = true` | Field required validation (`validation.Required`) |
| **CHECK** | `pg_constraint (contype = 'c')` | Field value ranges (`Min`, `Max`, regex pattern) |
| **UNIQUE** | `pg_constraint (contype = 'u')` / Unique Index | `collection.Indexes` with `CREATE UNIQUE INDEX` |
| **EXCLUSION** | `pg_constraint (contype = 'x')` | Advanced concurrency range locking |

---

## 7. Namespace & Schema Isolation

PostgreSQL natively organizes databases into **schemas** (`pg_namespace`):
- **`public`**: Default schema for standard collection tables.
- **`ruxbase_system`** (or `_` prefix in public): System tables (`_collections`, `_params`, `_migrations`, `_externalAuths`).
- **Custom Application Schemas:** User-defined schemas (`tenant_acme`, `crm`, `inventory`).
- **Introspection Scoping:** Catalog queries explicitly scope to `current_schemas(false)` or designated schema namespaces, preventing unintentional leakage of temporary tables or internal PostgreSQL system objects.
