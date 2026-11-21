# Ruxbase Wave 02 — Exact Numeric Types, Native Timestamps & Generated Columns

**Document Reference:** `docs/architecture/WAVE02-NUMERIC-TYPES-AND-GENERATED-COLUMNS.md`  
**Milestone:** W02-04 / P1 / CORE  
**Status:** **AUTHORITATIVE / FOUNDATION BASELINE**  
**Integration Branch:** `wave/02-postgresql-native-engine`  
**Compatibility Target:** PostgreSQL 17 and PostgreSQL 18  

---

## 1. Architectural Mission & Invariants

Ruxbase bridges application ergonomics with PostgreSQL's exact data representation guarantees:
1. **Exact Numeric Support (`NUMERIC(p,s)` / `DECIMAL(p,s)`):** Financial, accounting, and scientific domains require exact arbitrary-precision decimal arithmetic without IEEE-754 binary floating-point roundoff errors (e.g. `0.1 + 0.2 != 0.3`).
2. **Native Timestamps (`TIMESTAMPTZ` / `TIMESTAMP`):** Full roundtrip fidelity for timestamps with and without time zones, formatted in UTC ISO 8601 (`YYYY-MM-DDTHH:MM:SS.sssZ`).
3. **Generated Columns (`GENERATED ALWAYS AS (...) STORED`):** Full catalog introspection, automated schema synchronization, read-only API exposure, and omission from INSERT/UPDATE statements to satisfy PostgreSQL's non-default column modification constraints.
4. **Type-Safe Validation:** Deterministic validation rules enforce numeric scale (fractional digit limit) and precision (total significant digit limit) prior to persistence.

---

## 2. Exact Numeric Types Specification

### 2.1 Catalog Introspection & Type Modifiers
PostgreSQL stores `NUMERIC(p,s)` modifiers in `pg_attribute.atttypmod`:
```sql
numeric_precision = ((atttypmod - 4) >> 16) & 65535
numeric_scale     = (atttypmod - 4) & 65535
```
Ruxbase's `IntrospectTable` extracts these parameters directly from the catalog into `NativeColumn.NumericPrecision` and `NativeColumn.NumericScale`.

### 2.2 NumberField Extension
`NumberField` is extended with:
- `Precision *int`: Maximum total digits (1 to 1000).
- `Scale *int`: Number of fractional digits after decimal point (0 to precision).
- `ColumnType`:
  - If both `Precision` and `Scale` are configured: `NUMERIC(precision, scale) DEFAULT 0 NOT NULL`
  - If only `Precision` is configured: `NUMERIC(precision) DEFAULT 0 NOT NULL`
  - If `OnlyInt` is true: `BIGINT DEFAULT 0 NOT NULL`
  - Otherwise: `DOUBLE PRECISION DEFAULT 0 NOT NULL`

### 2.3 Validation Rules
- **Scale Violation:** If input value has more fractional digits than `*f.Scale`, validation fails with `validation_numeric_scale_exceeded`.
- **Precision Violation:** If input value has more significant digits than `*f.Precision`, validation fails with `validation_numeric_precision_exceeded`.

---

## 3. Native Timestamps Specification

- **PostgreSQL Storage:** `TIMESTAMPTZ` stores timestamp normalized to UTC internally.
- **Scanning:** `types.DateTime.Scan` accepts `time.Time` directly from the pgx driver without string serialization overhead.
- **Serialization:** Emits standard ISO 8601 formatted strings in UTC. Zero values are represented as `NULL`.

---

## 4. Generated Columns Specification

### 4.1 PostgreSQL Engine Semantics
PostgreSQL 17 and 18 support stored generated columns:
```sql
CREATE TABLE order_items (
    id text PRIMARY KEY,
    unit_price numeric(10, 2) NOT NULL,
    quantity integer NOT NULL,
    total_price numeric(12, 2) GENERATED ALWAYS AS (unit_price * quantity) STORED
);
```
Attempting to supply a non-DEFAULT value on `INSERT` or `UPDATE` results in error `cannot insert a non-DEFAULT value into column "total_price"` (SQLSTATE 42601).

### 4.2 Ruxbase Generated Column Handling
1. **Introspection:** `NativeColumn.Generated()` inspects `pg_attribute.attgenerated = 's'`.
2. **Field Contract:** Fields implement `interface{ IsGenerated() bool }`.
3. **Persistence Exclusion:** `Record.dbExport()` detects `field.IsGenerated()` and omits the column from INSERT and UPDATE statements, allowing PostgreSQL to compute the value.
4. **Validation Bypass:** `field.ValidateValue()` permits zero/empty values on generated columns during input validation since the engine populates them.
5. **Query Access:** Computed values are retrieved normally via `SELECT *` and accessible through `Record.Get()`, `Record.GetFloat()`, and JSON API responses.
