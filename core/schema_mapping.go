package core

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/AbdullahZaher/ruxbase/tools/security"
	"github.com/pocketbase/dbx"
)

// NativeColumn represents detailed PostgreSQL catalog metadata for a table column.
type NativeColumn struct {
	Name             string `db:"column_name"`
	OrdinalPosition  int    `db:"ordinal_position"`
	DataType         string `db:"data_type"`
	UDTName          string `db:"udt_name"`
	IsNullable       string `db:"is_nullable"` // "YES" or "NO"
	DefaultValue     string `db:"column_default"`
	CharMaxLength    *int   `db:"character_maximum_length"`
	NumericPrecision *int   `db:"numeric_precision"`
	NumericScale     *int   `db:"numeric_scale"`
	IsIdentity       string `db:"is_identity"` // "YES" or "NO"
	IsGenerated      string `db:"is_generated"`
	IsPrimaryKey     bool   `db:"is_primary_key"`
}

// Nullable reports whether the column permits NULL values.
func (c *NativeColumn) Nullable() bool {
	return strings.EqualFold(c.IsNullable, "YES")
}

// Identity reports whether the column is an identity column.
func (c *NativeColumn) Identity() bool {
	return strings.EqualFold(c.IsIdentity, "YES")
}

// Generated reports whether the column is a generated column.
func (c *NativeColumn) Generated() bool {
	return strings.EqualFold(c.IsGenerated, "YES") || c.IsGenerated == "ALWAYS" || c.IsGenerated == "s"
}

// NativeConstraint represents a table constraint in the PostgreSQL catalog.
type NativeConstraint struct {
	Name        string   `db:"constraint_name"`
	Type        string   `db:"constraint_type"` // "PRIMARY KEY", "FOREIGN KEY", "UNIQUE", "CHECK", "NOT NULL", "EXCLUSION"
	TableSchema string   `db:"table_schema"`
	TableName   string   `db:"table_name"`
	Columns     []string `db:"-"`
	RefTable    string   `db:"ref_table"`
	RefColumns  []string `db:"-"`
	OnDelete    string   `db:"on_delete"`
	OnUpdate    string   `db:"on_update"`
	CheckClause string   `db:"check_clause"`

	// Raw JSON fields scanned from catalog queries
	ColumnsJSON    string `db:"columns_json"`
	RefColumnsJSON string `db:"ref_columns_json"`
}

// NativeIndexInfo represents detailed index metadata from pg_index.
type NativeIndexInfo struct {
	Name        string   `db:"index_name"`
	IsUnique    bool     `db:"is_unique"`
	IsPrimary   bool     `db:"is_primary"`
	Method      string   `db:"method"` // "btree", "gin", "gist", "hnsw", "ivfflat", etc.
	Definition  string   `db:"definition"`
	Columns     []string `db:"-"`
	ColumnsJSON string   `db:"columns_json"`
}

// NativeTable represents a complete table or view definition introspected from PostgreSQL catalog.
type NativeTable struct {
	Schema           string              `db:"table_schema"`
	Name             string              `db:"table_name"`
	Kind             string              `db:"table_kind"` // 'r' table, 'v' view, 'm' matview, 'p' partition
	Owner            string              `db:"table_owner"`
	RowSecurity      bool                `db:"row_security"`
	ForceRowSecurity bool                `db:"force_row_security"`
	Columns          []*NativeColumn     `db:"-"`
	Constraints      []*NativeConstraint `db:"-"`
	Indexes          []*NativeIndexInfo  `db:"-"`
}

// PrimaryKeyColumns returns the names of all columns composing the primary key.
func (t *NativeTable) PrimaryKeyColumns() []string {
	var pks []string
	for _, col := range t.Columns {
		if col.IsPrimaryKey {
			pks = append(pks, col.Name)
		}
	}
	return pks
}

// ForeignKeys returns all foreign key constraints on the table.
func (t *NativeTable) ForeignKeys() []*NativeConstraint {
	var fks []*NativeConstraint
	for _, c := range t.Constraints {
		if c.Type == "FOREIGN KEY" {
			fks = append(fks, c)
		}
	}
	return fks
}

// UniqueConstraints returns all unique constraints on the table (excluding PK).
func (t *NativeTable) UniqueConstraints() []*NativeConstraint {
	var ucs []*NativeConstraint
	for _, c := range t.Constraints {
		if c.Type == "UNIQUE" {
			ucs = append(ucs, c)
		}
	}
	return ucs
}

// CheckConstraints returns all check constraints on the table.
func (t *NativeTable) CheckConstraints() []*NativeConstraint {
	var ccs []*NativeConstraint
	for _, c := range t.Constraints {
		if c.Type == "CHECK" {
			ccs = append(ccs, c)
		}
	}
	return ccs
}

// FindColumn returns the column with the specified name, or nil if not found.
func (t *NativeTable) FindColumn(name string) *NativeColumn {
	for _, col := range t.Columns {
		if strings.EqualFold(col.Name, name) {
			return col
		}
	}
	return nil
}

// MapNativeColumnToField maps a PostgreSQL NativeColumn into the most appropriate Collection Field.
func MapNativeColumnToField(col *NativeColumn) (Field, error) {
	if col == nil {
		return nil, errors.New("cannot map nil column to field")
	}

	fieldId := security.PseudorandomString(8)
	required := !col.Nullable()

	udt := strings.ToLower(col.UDTName)
	dataType := strings.ToLower(col.DataType)

	switch {
	case udt == "bool" || udt == "boolean":
		return &BoolField{
			Id:        fieldId,
			Name:      col.Name,
			Required:  required,
			Generated: col.Generated(),
		}, nil

	case strings.HasPrefix(udt, "int") || udt == "smallint" || udt == "integer" || udt == "bigint" ||
		udt == "float4" || udt == "float8" || udt == "numeric" || udt == "decimal" ||
		strings.Contains(dataType, "double precision") || strings.Contains(dataType, "real"):
		numField := &NumberField{
			Id:        fieldId,
			Name:      col.Name,
			Required:  required,
			Precision: col.NumericPrecision,
			Scale:     col.NumericScale,
			Generated: col.Generated(),
		}
		if strings.HasPrefix(udt, "int") || udt == "smallint" || udt == "integer" || udt == "bigint" {
			numField.OnlyInt = true
		}
		return numField, nil

	case strings.HasPrefix(udt, "timestamp") || udt == "timestamptz" || udt == "date" || udt == "time":
		return &DateField{
			Id:        fieldId,
			Name:      col.Name,
			Required:  required,
			Generated: col.Generated(),
		}, nil

	case udt == "json" || udt == "jsonb":
		return &JSONField{
			Id:        fieldId,
			Name:      col.Name,
			Required:  required,
			Generated: col.Generated(),
		}, nil

	case udt == "uuid":
		return &TextField{
			Id:        fieldId,
			Name:      col.Name,
			Required:  required,
			Generated: col.Generated(),
			Pattern:   `^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
		}, nil

	case udt == "vector":
		return &VectorField{
			Id:       fieldId,
			Name:     col.Name,
			Required: required,
		}, nil

	case udt == "point":
		return &GeoPointField{
			Id:       fieldId,
			Name:     col.Name,
			Required: required,
		}, nil

	default:
		// varchar, text, bpchar, or other standard text representations
		var maxLen int
		if col.CharMaxLength != nil && *col.CharMaxLength > 0 {
			maxLen = *col.CharMaxLength
		}
		return &TextField{
			Id:        fieldId,
			Name:      col.Name,
			Required:  required,
			Max:       maxLen,
			Generated: col.Generated(),
		}, nil
	}
}

// MapFieldToNativeColumn converts a Collection Field into its expected native PostgreSQL column definition.
func MapFieldToNativeColumn(field Field) (*NativeColumn, error) {
	if field == nil {
		return nil, errors.New("cannot map nil field to column")
	}

	col := &NativeColumn{
		Name:         field.GetName(),
		IsNullable:   "NO",
		IsPrimaryKey: field.GetName() == FieldNameId,
	}

	switch f := field.(type) {
	case *BoolField:
		col.DataType = "boolean"
		col.UDTName = "bool"
		col.DefaultValue = "false"
	case *NumberField:
		if f.Precision != nil && f.Scale != nil {
			col.DataType = fmt.Sprintf("numeric(%d,%d)", *f.Precision, *f.Scale)
			col.UDTName = "numeric"
			col.NumericPrecision = f.Precision
			col.NumericScale = f.Scale
		} else if f.Precision != nil {
			col.DataType = fmt.Sprintf("numeric(%d)", *f.Precision)
			col.UDTName = "numeric"
			col.NumericPrecision = f.Precision
		} else if f.OnlyInt {
			col.DataType = "bigint"
			col.UDTName = "int8"
		} else {
			col.DataType = "double precision"
			col.UDTName = "float8"
		}
		col.DefaultValue = "0"
	case *DateField, *AutodateField:
		col.DataType = "timestamp with time zone"
		col.UDTName = "timestamptz"
		col.IsNullable = "YES"
	case *JSONField:
		col.DataType = "jsonb"
		col.UDTName = "jsonb"
		col.DefaultValue = "'null'::jsonb"
	case *VectorField:
		col.DataType = fmt.Sprintf("vector(%d)", f.Dimensions)
		col.UDTName = "vector"
	case *GeoPointField:
		col.DataType = "point"
		col.UDTName = "point"
	case *RelationField:
		if f.IsMultiple() {
			col.DataType = "jsonb"
			col.UDTName = "jsonb"
			col.DefaultValue = "'[]'::jsonb"
		} else {
			col.DataType = "text"
			col.UDTName = "text"
			col.DefaultValue = "''"
		}
	default:
		col.DataType = "text"
		col.UDTName = "text"
		col.DefaultValue = "''"
	}

	return col, nil
}

// IntrospectTable returns detailed PostgreSQL catalog metadata, columns, constraints, and indexes for a table.
func (app *BaseApp) IntrospectTable(tableName string) (*NativeTable, error) {
	table := &NativeTable{}

	err := app.ConcurrentDB().NewQuery(`
		SELECT
			n.[[nspname]] AS [[table_schema]],
			c.[[relname]] AS [[table_name]],
			c.[[relkind]]::text AS [[table_kind]],
			pg_get_userbyid(c.[[relowner]]) AS [[table_owner]],
			c.[[relrowsecurity]] AS [[row_security]],
			c.[[relforcerowsecurity]] AS [[force_row_security]]
		FROM {{pg_class}} c
		JOIN {{pg_namespace}} n ON n.[[oid]] = c.[[relnamespace]]
		WHERE c.[[oid]] = to_regclass(quote_ident({:tableName}))
		LIMIT 1
	`).
		Bind(dbx.Params{"tableName": tableName}).
		One(table)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect table %q: %w", tableName, err)
	}

	// Columns
	cols, err := app.introspectTableColumns(tableName)
	if err != nil {
		return nil, err
	}
	table.Columns = cols

	// Constraints
	constrs, err := app.IntrospectTableConstraints(tableName)
	if err != nil {
		return nil, err
	}
	table.Constraints = constrs

	// Indexes
	indexes, err := app.IntrospectTableIndexes(tableName)
	if err != nil {
		return nil, err
	}
	table.Indexes = indexes

	return table, nil
}

func (app *BaseApp) introspectTableColumns(tableName string) ([]*NativeColumn, error) {
	columns := []*NativeColumn{}

	err := app.ConcurrentDB().NewQuery(`
		SELECT
			a.[[attname]] AS [[column_name]],
			a.[[attnum]] AS [[ordinal_position]],
			format_type(a.[[atttypid]], a.[[atttypmod]]) AS [[data_type]],
			t.[[typname]] AS [[udt_name]],
			CASE WHEN a.[[attnotnull]] THEN 'NO' ELSE 'YES' END AS [[is_nullable]],
			COALESCE(pg_get_expr(d.[[adbin]], d.[[adrelid]]), '') AS [[column_default]],
			CASE WHEN a.[[attidentity]] != '' THEN 'YES' ELSE 'NO' END AS [[is_identity]],
			CASE WHEN a.[[attgenerated]] != '' THEN 'YES' ELSE 'NO' END AS [[is_generated]],
			CASE WHEN a.[[atttypmod]] > 4 THEN ((a.[[atttypmod]] - 4) >> 16) & 65535 ELSE NULL END AS [[numeric_precision]],
			CASE WHEN a.[[atttypmod]] > 4 THEN (a.[[atttypmod]] - 4) & 65535 ELSE NULL END AS [[numeric_scale]],
			EXISTS (
				SELECT 1
				FROM {{pg_index}} i
				WHERE i.[[indrelid]] = a.[[attrelid]]
					AND i.[[indisprimary]]
					AND a.[[attnum]] = ANY(i.[[indkey]])
			) AS [[is_primary_key]]
		FROM {{pg_attribute}} a
		JOIN {{pg_type}} t ON t.[[oid]] = a.[[atttypid]]
		LEFT JOIN {{pg_attrdef}} d ON d.[[adrelid]] = a.[[attrelid]] AND d.[[adnum]] = a.[[attnum]]
		WHERE a.[[attrelid]] = to_regclass(quote_ident({:tableName}))
			AND a.[[attnum]] > 0
			AND NOT a.[[attisdropped]]
		ORDER BY a.[[attnum]]
	`).
		Bind(dbx.Params{"tableName": tableName}).
		All(&columns)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect columns for %q: %w", tableName, err)
	}

	return columns, nil
}

// IntrospectTableConstraints returns all constraints defined on the specified table.
func (app *BaseApp) IntrospectTableConstraints(tableName string) ([]*NativeConstraint, error) {
	constrs := []*NativeConstraint{}

	err := app.ConcurrentDB().NewQuery(`
		SELECT
			c.[[conname]] AS [[constraint_name]],
			CASE c.[[contype]]
				WHEN 'p' THEN 'PRIMARY KEY'
				WHEN 'f' THEN 'FOREIGN KEY'
				WHEN 'u' THEN 'UNIQUE'
				WHEN 'c' THEN 'CHECK'
				WHEN 'n' THEN 'NOT NULL'
				WHEN 'x' THEN 'EXCLUSION'
				ELSE c.[[contype]]::text
			END AS [[constraint_type]],
			n.[[nspname]] AS [[table_schema]],
			cl.[[relname]] AS [[table_name]],
			COALESCE((
				SELECT json_agg(att.[[attname]] ORDER BY u.[[ord]])
				FROM UNNEST(c.[[conkey]]) WITH ORDINALITY AS u([[attnum]], [[ord]])
				JOIN {{pg_attribute}} att ON att.[[attrelid]] = c.[[conrelid]] AND att.[[attnum]] = u.[[attnum]]
			)::text, '[]') AS [[columns_json]],
			COALESCE(ref_cl.[[relname]], '') AS [[ref_table]],
			COALESCE((
				SELECT json_agg(ref_att.[[attname]] ORDER BY ref_u.[[ord]])
				FROM UNNEST(c.[[confkey]]) WITH ORDINALITY AS ref_u([[attnum]], [[ord]])
				JOIN {{pg_attribute}} ref_att ON ref_att.[[attrelid]] = c.[[confrelid]] AND ref_att.[[attnum]] = ref_u.[[attnum]]
			)::text, '[]') AS [[ref_columns_json]],
			CASE c.[[confdeltype]]
				WHEN 'a' THEN 'NO ACTION'
				WHEN 'r' THEN 'RESTRICT'
				WHEN 'c' THEN 'CASCADE'
				WHEN 'n' THEN 'SET NULL'
				WHEN 'd' THEN 'SET DEFAULT'
				ELSE ''
			END AS [[on_delete]],
			CASE c.[[confupdtype]]
				WHEN 'a' THEN 'NO ACTION'
				WHEN 'r' THEN 'RESTRICT'
				WHEN 'c' THEN 'CASCADE'
				WHEN 'n' THEN 'SET NULL'
				WHEN 'd' THEN 'SET DEFAULT'
				ELSE ''
			END AS [[on_update]],
			COALESCE(pg_get_expr(c.[[conbin]], c.[[conrelid]]), '') AS [[check_clause]]
		FROM {{pg_constraint}} c
		JOIN {{pg_namespace}} n ON n.[[oid]] = c.[[connamespace]]
		JOIN {{pg_class}} cl ON cl.[[oid]] = c.[[conrelid]]
		LEFT JOIN {{pg_class}} ref_cl ON ref_cl.[[oid]] = c.[[confrelid]]
		WHERE c.[[conrelid]] = to_regclass(quote_ident({:tableName}))
		ORDER BY c.[[conname]]
	`).
		Bind(dbx.Params{"tableName": tableName}).
		All(&constrs)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect constraints for %q: %w", tableName, err)
	}

	for _, c := range constrs {
		if c.ColumnsJSON != "" {
			_ = json.Unmarshal([]byte(c.ColumnsJSON), &c.Columns)
		}
		if c.RefColumnsJSON != "" {
			_ = json.Unmarshal([]byte(c.RefColumnsJSON), &c.RefColumns)
		}
	}

	return constrs, nil
}

// IntrospectTableIndexes returns all indexes defined on the specified table with detailed definitions.
func (app *BaseApp) IntrospectTableIndexes(tableName string) ([]*NativeIndexInfo, error) {
	indexes := []*NativeIndexInfo{}

	err := app.ConcurrentDB().NewQuery(`
		SELECT
			c.[[relname]] AS [[index_name]],
			i.[[indisunique]] AS [[is_unique]],
			i.[[indisprimary]] AS [[is_primary]],
			am.[[amname]] AS [[method]],
			pg_get_indexdef(i.[[indexrelid]]) AS [[definition]],
			COALESCE((
				SELECT json_agg(att.[[attname]] ORDER BY u.[[ord]])
				FROM UNNEST(i.[[indkey]]) WITH ORDINALITY AS u([[attnum]], [[ord]])
				JOIN {{pg_attribute}} att ON att.[[attrelid]] = i.[[indrelid]] AND att.[[attnum]] = u.[[attnum]]
			)::text, '[]') AS [[columns_json]]
		FROM {{pg_index}} i
		JOIN {{pg_class}} c ON c.[[oid]] = i.[[indexrelid]]
		JOIN {{pg_am}} am ON am.[[oid]] = c.[[relam]]
		WHERE i.[[indrelid]] = to_regclass(quote_ident({:tableName}))
		ORDER BY c.[[relname]]
	`).
		Bind(dbx.Params{"tableName": tableName}).
		All(&indexes)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect indexes for %q: %w", tableName, err)
	}

	for _, idx := range indexes {
		if idx.ColumnsJSON != "" {
			_ = json.Unmarshal([]byte(idx.ColumnsJSON), &idx.Columns)
		}
	}

	return indexes, nil
}

// IntrospectSchemaTables returns all tables, views, and partitioned tables within the specified schema.
func (app *BaseApp) IntrospectSchemaTables(schema string) ([]*NativeTable, error) {
	if schema == "" {
		schema = "public"
	}

	tables := []*NativeTable{}

	err := app.ConcurrentDB().NewQuery(`
		SELECT
			n.[[nspname]] AS [[table_schema]],
			c.[[relname]] AS [[table_name]],
			c.[[relkind]]::text AS [[table_kind]],
			pg_get_userbyid(c.[[relowner]]) AS [[table_owner]],
			c.[[relrowsecurity]] AS [[row_security]],
			c.[[relforcerowsecurity]] AS [[force_row_security]]
		FROM {{pg_class}} c
		JOIN {{pg_namespace}} n ON n.[[oid]] = c.[[relnamespace]]
		WHERE n.[[nspname]] = {:schema}
			AND c.[[relkind]] IN ('r', 'p', 'v', 'm')
		ORDER BY c.[[relname]]
	`).
		Bind(dbx.Params{"schema": schema}).
		All(&tables)
	if err != nil {
		return nil, fmt.Errorf("failed to introspect tables in schema %q: %w", schema, err)
	}

	return tables, nil
}
