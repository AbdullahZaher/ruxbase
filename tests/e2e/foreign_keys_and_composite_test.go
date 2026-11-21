package e2e_test

import (
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNativeForeignKeys_EnforcedAtEngineLevel(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create parent collection "authors"
	authorsCol := core.NewBaseCollection("authors")
	authorsCol.Fields.Add(&core.TextField{Name: "name", Required: true})
	if err := app.Save(authorsCol); err != nil {
		t.Fatalf("Failed to save authors collection: %v", err)
	}

	author := core.NewRecord(authorsCol)
	author.Set("name", "Arthur Conan Doyle")
	if err := app.Save(author); err != nil {
		t.Fatalf("Failed to create author: %v", err)
	}

	// 2. Create child collection "books" with NativeFK = true, OnDelete = "RESTRICT"
	booksCol := core.NewBaseCollection("books")
	booksCol.Fields.Add(&core.TextField{Name: "title", Required: true})
	booksCol.Fields.Add(&core.RelationField{
		Name:         "author",
		CollectionId: authorsCol.Id,
		NativeFK:     true,
		OnDelete:     "RESTRICT",
		Required:     true,
		MaxSelect:    1,
	})
	if err := app.Save(booksCol); err != nil {
		t.Fatalf("Failed to save books collection with native FK: %v", err)
	}

	// Verify that the foreign key constraint exists in PostgreSQL catalog
	constrs, err := app.IntrospectTableConstraints("books")
	if err != nil {
		t.Fatalf("Failed to introspect books constraints: %v", err)
	}

	var foundFK bool
	for _, c := range constrs {
		if c.Type == "FOREIGN KEY" && c.RefTable == "authors" {
			foundFK = true
			if c.OnDelete != "RESTRICT" {
				t.Errorf("Expected ON DELETE RESTRICT, got %q", c.OnDelete)
			}
			break
		}
	}
	if !foundFK {
		t.Fatalf("Native foreign key constraint from books to authors was not found in catalog")
	}

	// 3. Test engine-level FK enforcement on insert with invalid author via direct SQL
	_, rawErr := app.DB().NewQuery("INSERT INTO books (id, title, author) VALUES ('invalidbook0001', 'Direct SQL Book', 'non_existent_author')").Execute()
	if rawErr == nil {
		t.Fatalf("Expected direct SQL insert with invalid author FK to fail, but succeeded")
	}

	var pgErr *pgconn.PgError
	if errors.As(rawErr, &pgErr) {
		if pgErr.Code != "23503" { // foreign_key_violation
			t.Errorf("Expected PostgreSQL SQLSTATE 23503 (foreign_key_violation), got %s: %v", pgErr.Code, rawErr)
		}
	} else {
		t.Errorf("Expected pgconn.PgError, got: %v", rawErr)
	}

	// Also verify that application-level Save fails
	invalidBook := core.NewRecord(booksCol)
	invalidBook.Set("title", "A Mystery")
	invalidBook.Set("author", "nonexistent0001")
	if err := app.Save(invalidBook); err == nil {
		t.Fatalf("Expected saving book with invalid author FK to fail, but succeeded")
	}

	// 4. Insert valid book
	validBook := core.NewRecord(booksCol)
	validBook.Set("title", "A Study in Scarlet")
	validBook.Set("author", author.Id)
	if err := app.Save(validBook); err != nil {
		t.Fatalf("Failed to save valid book: %v", err)
	}

	// 5. Negative check: deleting parent author with child book referencing it must fail with RESTRICT (SQLSTATE 23503)
	delErr := app.Delete(author)
	if delErr == nil {
		t.Fatalf("Expected deleting author with referencing book under RESTRICT to fail, but succeeded")
	}

	if errors.As(delErr, &pgErr) {
		// PostgreSQL returns 23001 for RESTRICT violations and 23503 for foreign_key_violation
		if pgErr.Code != "23001" && pgErr.Code != "23503" {
			t.Errorf("Expected PostgreSQL SQLSTATE 23001 or 23503 on delete restrict, got %s: %v", pgErr.Code, delErr)
		}
	} else if !strings.Contains(delErr.Error(), "RESTRICT") && !strings.Contains(delErr.Error(), "foreign key") {
		t.Errorf("Expected foreign key or restrict violation error, got: %v", delErr)
	}
}

func TestNativeForeignKeys_CascadeDeletion(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create parent collection "departments"
	deptsCol := core.NewBaseCollection("departments")
	deptsCol.Fields.Add(&core.TextField{Name: "name", Required: true})
	if err := app.Save(deptsCol); err != nil {
		t.Fatalf("Failed to save departments collection: %v", err)
	}

	dept := core.NewRecord(deptsCol)
	dept.Set("name", "Engineering")
	if err := app.Save(dept); err != nil {
		t.Fatalf("Failed to create department: %v", err)
	}

	// 2. Create child collection "employees" with NativeFK = true, CascadeDelete = true
	empsCol := core.NewBaseCollection("employees")
	empsCol.Fields.Add(&core.TextField{Name: "name", Required: true})
	empsCol.Fields.Add(&core.RelationField{
		Name:          "department",
		CollectionId:  deptsCol.Id,
		NativeFK:      true,
		CascadeDelete: true,
		OnDelete:      "CASCADE",
		Required:      true,
		MaxSelect:     1,
	})
	if err := app.Save(empsCol); err != nil {
		t.Fatalf("Failed to save employees collection: %v", err)
	}

	emp1 := core.NewRecord(empsCol)
	emp1.Set("name", "Alice")
	emp1.Set("department", dept.Id)
	if err := app.Save(emp1); err != nil {
		t.Fatalf("Failed to create emp1: %v", err)
	}

	emp2 := core.NewRecord(empsCol)
	emp2.Set("name", "Bob")
	emp2.Set("department", dept.Id)
	if err := app.Save(emp2); err != nil {
		t.Fatalf("Failed to create emp2: %v", err)
	}

	// Verify both employees exist
	count, err := app.CountRecords(empsCol)
	if err != nil || count != 2 {
		t.Fatalf("Expected 2 employees, got %d (err: %v)", count, err)
	}

	// 3. Delete parent department
	if err := app.Delete(dept); err != nil {
		t.Fatalf("Failed to delete department: %v", err)
	}

	// 4. Verify cascade: both child employees must be gone
	remainingCount, err := app.CountRecords(empsCol)
	if err != nil {
		t.Fatalf("Failed to count remaining employees: %v", err)
	}
	if remainingCount != 0 {
		t.Fatalf("Expected 0 employees remaining after cascade delete, got %d", remainingCount)
	}
}

func TestCompositeKey_IntrospectionAndQuery(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create a native table with composite primary key (student_id, course_id)
	setupSQL := `
		CREATE TABLE course_enrollments (
			student_id varchar(50) NOT NULL,
			course_id varchar(50) NOT NULL,
			grade varchar(5) DEFAULT 'A',
			enrolled_at timestamptz DEFAULT now(),
			PRIMARY KEY (student_id, course_id)
		);
		INSERT INTO course_enrollments (student_id, course_id, grade) VALUES ('s101', 'cs101', 'A');
		INSERT INTO course_enrollments (student_id, course_id, grade) VALUES ('s101', 'math201', 'B+');
		INSERT INTO course_enrollments (student_id, course_id, grade) VALUES ('s102', 'cs101', 'A-');
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to setup course_enrollments: %v", err)
	}

	// 2. Introspect table and assert composite PK
	tbl, err := app.IntrospectTable("course_enrollments")
	if err != nil {
		t.Fatalf("IntrospectTable failed: %v", err)
	}

	pks := tbl.PrimaryKeyColumns()
	if len(pks) != 2 {
		t.Fatalf("Expected 2 composite primary key columns, got %v", pks)
	}
	if !strings.EqualFold(pks[0], "student_id") && !strings.EqualFold(pks[1], "student_id") {
		t.Errorf("Expected student_id in composite PK, got %v", pks)
	}
	if !strings.EqualFold(pks[0], "course_id") && !strings.EqualFold(pks[1], "course_id") {
		t.Errorf("Expected course_id in composite PK, got %v", pks)
	}

	// 3. Expose table as Collection
	listRule := "" // public list
	col, err := app.ExposeNativeTable(core.ExposeTableOptions{
		TableName:      "course_enrollments",
		CollectionName: "course_enrollments",
		ListRule:       &listRule,
	})
	if err != nil {
		t.Fatalf("ExposeNativeTable failed: %v", err)
	}
	if col.Name != "course_enrollments" {
		t.Errorf("Expected collection name 'course_enrollments', got %q", col.Name)
	}

	// 4. Test FindRecordByCompositeKey for exact match
	record, err := app.FindRecordByCompositeKey("course_enrollments", map[string]any{
		"student_id": "s101",
		"course_id":  "math201",
	})
	if err != nil {
		t.Fatalf("FindRecordByCompositeKey failed: %v", err)
	}

	if record.GetString("student_id") != "s101" || record.GetString("course_id") != "math201" {
		t.Errorf("Unexpected record composite keys: student=%q, course=%q",
			record.GetString("student_id"), record.GetString("course_id"))
	}
	if record.GetString("grade") != "B+" {
		t.Errorf("Expected grade 'B+', got %q", record.GetString("grade"))
	}

	// 5. Test FindRecordByCompositeKey with non-existent pair returns sql.ErrNoRows
	_, err = app.FindRecordByCompositeKey("course_enrollments", map[string]any{
		"student_id": "s101",
		"course_id":  "physics999",
	})
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Expected sql.ErrNoRows for non-existent composite key, got: %v", err)
	}
}
