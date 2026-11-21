package core_test

import (
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
)

func TestMapNativeColumnToField(t *testing.T) {
	scenarios := []struct {
		name         string
		col          *core.NativeColumn
		expectedType string
		required     bool
		hasPattern   bool
	}{
		{
			name: "boolean nullable",
			col: &core.NativeColumn{
				Name:       "is_active",
				DataType:   "boolean",
				UDTName:    "bool",
				IsNullable: "YES",
			},
			expectedType: core.FieldTypeBool,
			required:     false,
		},
		{
			name: "integer not null",
			col: &core.NativeColumn{
				Name:       "counter",
				DataType:   "integer",
				UDTName:    "int4",
				IsNullable: "NO",
			},
			expectedType: core.FieldTypeNumber,
			required:     true,
		},
		{
			name: "numeric exact decimal",
			col: &core.NativeColumn{
				Name:       "amount",
				DataType:   "numeric(10,2)",
				UDTName:    "numeric",
				IsNullable: "NO",
			},
			expectedType: core.FieldTypeNumber,
			required:     true,
		},
		{
			name: "timestamp with time zone",
			col: &core.NativeColumn{
				Name:       "published_at",
				DataType:   "timestamp with time zone",
				UDTName:    "timestamptz",
				IsNullable: "YES",
			},
			expectedType: core.FieldTypeDate,
			required:     false,
		},
		{
			name: "jsonb binary",
			col: &core.NativeColumn{
				Name:       "metadata",
				DataType:   "jsonb",
				UDTName:    "jsonb",
				IsNullable: "NO",
			},
			expectedType: core.FieldTypeJSON,
			required:     true,
		},
		{
			name: "uuid type",
			col: &core.NativeColumn{
				Name:       "uuid_id",
				DataType:   "uuid",
				UDTName:    "uuid",
				IsNullable: "NO",
			},
			expectedType: core.FieldTypeText,
			required:     true,
			hasPattern:   true,
		},
		{
			name: "vector embedding",
			col: &core.NativeColumn{
				Name:       "embedding",
				DataType:   "vector(1536)",
				UDTName:    "vector",
				IsNullable: "YES",
			},
			expectedType: core.FieldTypeVector,
			required:     false,
		},
		{
			name: "point coordinates",
			col: &core.NativeColumn{
				Name:       "location",
				DataType:   "point",
				UDTName:    "point",
				IsNullable: "YES",
			},
			expectedType: core.FieldTypeGeoPoint,
			required:     false,
		},
		{
			name: "text with max length",
			col: &core.NativeColumn{
				Name:          "title",
				DataType:      "character varying(100)",
				UDTName:       "varchar",
				IsNullable:    "NO",
				CharMaxLength: func() *int { i := 100; return &i }(),
			},
			expectedType: core.FieldTypeText,
			required:     true,
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			field, err := core.MapNativeColumnToField(s.col)
			if err != nil {
				t.Fatalf("Unexpected error mapping column %s: %v", s.name, err)
			}

			if field.Type() != s.expectedType {
				t.Fatalf("Expected field type %q, got %q", s.expectedType, field.Type())
			}

			if field.GetName() != s.col.Name {
				t.Fatalf("Expected field name %q, got %q", s.col.Name, field.GetName())
			}

			if s.hasPattern {
				tf, ok := field.(*core.TextField)
				if !ok || tf.Pattern == "" {
					t.Fatalf("Expected non-empty pattern for UUID text field")
				}
			}
		})
	}
}

func TestMapFieldToNativeColumn(t *testing.T) {
	scenarios := []struct {
		name            string
		field           core.Field
		expectedUDT     string
		expectedDefault string
	}{
		{
			name:            "bool field",
			field:           &core.BoolField{Name: "active"},
			expectedUDT:     "bool",
			expectedDefault: "false",
		},
		{
			name:            "number field",
			field:           &core.NumberField{Name: "price"},
			expectedUDT:     "float8",
			expectedDefault: "0",
		},
		{
			name:            "date field",
			field:           &core.DateField{Name: "created"},
			expectedUDT:     "timestamptz",
			expectedDefault: "",
		},
		{
			name:            "json field",
			field:           &core.JSONField{Name: "config"},
			expectedUDT:     "jsonb",
			expectedDefault: "'null'::jsonb",
		},
		{
			name:            "single relation",
			field:           &core.RelationField{Name: "user", MaxSelect: 1},
			expectedUDT:     "text",
			expectedDefault: "''",
		},
		{
			name:            "multiple relation",
			field:           &core.RelationField{Name: "tags", MaxSelect: 5},
			expectedUDT:     "jsonb",
			expectedDefault: "'[]'::jsonb",
		},
		{
			name:            "vector field",
			field:           &core.VectorField{Name: "embedding", Dimensions: 768},
			expectedUDT:     "vector",
			expectedDefault: "",
		},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			col, err := core.MapFieldToNativeColumn(s.field)
			if err != nil {
				t.Fatalf("Unexpected error mapping field %s: %v", s.name, err)
			}

			if col.Name != s.field.GetName() {
				t.Fatalf("Expected column name %q, got %q", s.field.GetName(), col.Name)
			}

			if col.UDTName != s.expectedUDT {
				t.Fatalf("Expected UDT %q, got %q", s.expectedUDT, col.UDTName)
			}

			if s.expectedDefault != "" && col.DefaultValue != s.expectedDefault {
				t.Fatalf("Expected default %q, got %q", s.expectedDefault, col.DefaultValue)
			}
		})
	}
}

func TestIntrospectTable_Integration(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp failed: %v", err)
	}
	defer app.Cleanup()

	// Create test tables with constraints and indexes
	setupSQL := `
		CREATE TABLE test_parent_authors (
			id text PRIMARY KEY,
			name varchar(100) NOT NULL
		);

		CREATE TABLE test_native_articles (
			id text PRIMARY KEY,
			author_id text NOT NULL,
			title varchar(200) NOT NULL,
			views integer DEFAULT 0,
			rating numeric(3,2) DEFAULT 0.00,
			is_published boolean DEFAULT false,
			content text DEFAULT '',
			metadata jsonb DEFAULT '{}',
			created timestamptz DEFAULT now(),
			CONSTRAINT fk_article_author FOREIGN KEY (author_id)
				REFERENCES test_parent_authors(id) ON DELETE CASCADE ON UPDATE RESTRICT,
			CONSTRAINT chk_article_views CHECK (views >= 0)
		);

		CREATE UNIQUE INDEX idx_article_author_title ON test_native_articles(author_id, title);
		CREATE INDEX idx_article_views ON test_native_articles USING btree (views);

		ALTER TABLE test_native_articles ENABLE ROW LEVEL SECURITY;
		ALTER TABLE test_native_articles FORCE ROW LEVEL SECURITY;
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to create test setup tables: %v", err)
	}

	table, err := app.IntrospectTable("test_native_articles")
	if err != nil {
		t.Fatalf("IntrospectTable failed: %v", err)
	}

	if table.Name != "test_native_articles" {
		t.Errorf("Expected table name 'test_native_articles', got %q", table.Name)
	}
	if table.Kind != "r" {
		t.Errorf("Expected table kind 'r' (regular table), got %q", table.Kind)
	}
	if !table.RowSecurity {
		t.Errorf("Expected RowSecurity to be true")
	}
	if !table.ForceRowSecurity {
		t.Errorf("Expected ForceRowSecurity to be true")
	}

	// Columns assertions
	pks := table.PrimaryKeyColumns()
	if len(pks) != 1 || pks[0] != "id" {
		t.Errorf("Expected single PK 'id', got %v", pks)
	}

	authorCol := table.FindColumn("author_id")
	if authorCol == nil || authorCol.Nullable() {
		t.Errorf("Expected author_id to exist and be not null")
	}

	ratingCol := table.FindColumn("rating")
	if ratingCol == nil || !strings.Contains(ratingCol.DataType, "numeric") {
		t.Errorf("Expected rating column with numeric type, got %v", ratingCol)
	}

	// Foreign keys assertions
	fks := table.ForeignKeys()
	if len(fks) != 1 {
		t.Fatalf("Expected 1 foreign key, got %d", len(fks))
	}
	fk := fks[0]
	if fk.RefTable != "test_parent_authors" {
		t.Errorf("Expected FK ref table 'test_parent_authors', got %q", fk.RefTable)
	}
	if fk.OnDelete != "CASCADE" {
		t.Errorf("Expected ON DELETE CASCADE, got %q", fk.OnDelete)
	}
	if fk.OnUpdate != "RESTRICT" {
		t.Errorf("Expected ON UPDATE RESTRICT, got %q", fk.OnUpdate)
	}

	// Check constraints assertions
	chks := table.CheckConstraints()
	if len(chks) < 1 {
		t.Errorf("Expected at least 1 check constraint, got %d", len(chks))
	}
	var foundViewsCheck bool
	for _, c := range chks {
		if strings.Contains(c.CheckClause, "views >= 0") {
			foundViewsCheck = true
			break
		}
	}
	if !foundViewsCheck {
		t.Errorf("Expected check constraint with 'views >= 0'")
	}

	// Indexes assertions
	var foundUniqueAuthorTitle bool
	var foundViewsIndex bool
	for _, idx := range table.Indexes {
		if idx.Name == "idx_article_author_title" && idx.IsUnique {
			foundUniqueAuthorTitle = true
		}
		if idx.Name == "idx_article_views" && !idx.IsUnique && idx.Method == "btree" {
			foundViewsIndex = true
		}
	}
	if !foundUniqueAuthorTitle {
		t.Errorf("Expected unique composite index idx_article_author_title")
	}
	if !foundViewsIndex {
		t.Errorf("Expected btree index idx_article_views")
	}
}

func TestIntrospectSchemaTables_Integration(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("NewTestApp failed: %v", err)
	}
	defer app.Cleanup()

	tables, err := app.IntrospectSchemaTables("public")
	if err != nil {
		t.Fatalf("IntrospectSchemaTables failed: %v", err)
	}

	if len(tables) == 0 {
		t.Fatalf("Expected at least system tables in public schema, got 0")
	}

	var foundSuperusers bool
	for _, tbl := range tables {
		if tbl.Name == core.CollectionNameSuperusers {
			foundSuperusers = true
			break
		}
	}

	if !foundSuperusers {
		t.Errorf("Expected to find %q in introspected public schema tables", core.CollectionNameSuperusers)
	}
}
