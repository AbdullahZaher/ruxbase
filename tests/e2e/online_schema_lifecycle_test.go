package e2e_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/AbdullahZaher/ruxbase/tests"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestOnlineSchemaEvolution_CompleteFourPhaseLifecycle(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 0. Setup initial table with historical rows
	setupSQL := `
		CREATE TABLE members (
			id text PRIMARY KEY,
			first_name text NOT NULL,
			last_name text NOT NULL,
			status text DEFAULT 'active'
		);
		INSERT INTO members (id, first_name, last_name) VALUES
			('m01', 'Ada', 'Lovelace'),
			('m02', 'Grace', 'Hopper'),
			('m03', 'Margaret', 'Hamilton'),
			('m04', 'Katherine', 'Johnson'),
			('m05', 'Hedy', 'Lamarr');
	`
	if _, err := app.DB().NewQuery(setupSQL).Execute(); err != nil {
		t.Fatalf("Failed to setup members table: %v", err)
	}

	// ==============================================================
	// Phase 1: EXPAND — Add new column nullable under bounded lock
	// ==============================================================
	expandStep := core.OnlineEvolutionStep{
		Phase:         core.PhaseExpand,
		Name:          "add_full_name_column",
		LockTimeoutMs: 1500,
		UpSQL:         "ALTER TABLE members ADD COLUMN full_name text;",
		DownSQL:       "ALTER TABLE members DROP COLUMN IF EXISTS full_name;",
	}
	if err := app.ExecuteOnlineDDL(expandStep); err != nil {
		t.Fatalf("Phase 1 Expand failed: %v", err)
	}

	// Verify column exists and is NULL for existing records
	tbl, err := app.IntrospectTable("members")
	if err != nil || tbl.FindColumn("full_name") == nil {
		t.Fatalf("full_name column not found after expand phase")
	}

	// ==============================================================
	// Phase 2: BACKFILL — Chunked batch population in slices of 2
	// ==============================================================
	backfillCfg := core.BackfillConfig{
		TableName:    "members",
		PrimaryKey:   "id",
		SourceColumn: "first_name",
		TargetColumn: "full_name",
		TransformSQL: "first_name || ' ' || last_name",
		BatchSize:    2,
	}
	res, err := app.ExecuteChunkedBackfill(backfillCfg)
	if err != nil {
		t.Fatalf("Phase 2 Backfill failed: %v", err)
	}
	if res.TotalRowsUpdated != 5 {
		t.Errorf("Expected 5 rows backfilled, got %d", res.TotalRowsUpdated)
	}
	if res.BatchesRun < 3 {
		t.Errorf("Expected at least 3 batches for 5 rows with batch size 2, got %d", res.BatchesRun)
	}

	// Verify all members have expected full_name
	var unpopulatedCount int
	err = app.DB().NewQuery("SELECT count(*) FROM members WHERE full_name IS NULL").Row(&unpopulatedCount)
	if err != nil || unpopulatedCount != 0 {
		t.Fatalf("Expected 0 unpopulated rows after backfill, got %d (err: %v)", unpopulatedCount, err)
	}

	var adaFullName string
	err = app.DB().NewQuery("SELECT full_name FROM members WHERE id = 'm01'").Row(&adaFullName)
	if err != nil || adaFullName != "Ada Lovelace" {
		t.Errorf("Expected 'Ada Lovelace', got %q", adaFullName)
	}

	// ==============================================================
	// Phase 3: VALIDATE — Add NOT VALID constraint, then validate
	// ==============================================================
	constraintStep := core.OnlineEvolutionStep{
		Phase:         core.PhaseValidate,
		Name:          "add_full_name_length_constraint",
		LockTimeoutMs: 1500,
		UpSQL:         "ALTER TABLE members ADD CONSTRAINT check_full_name_len CHECK (length(full_name) > 0) NOT VALID;",
		DownSQL:       "ALTER TABLE members DROP CONSTRAINT IF EXISTS check_full_name_len;",
	}
	if err := app.ExecuteOnlineDDL(constraintStep); err != nil {
		t.Fatalf("Phase 3 add constraint failed: %v", err)
	}

	// Validate non-blocking
	if err := app.ValidateConstraintNonBlocking("members", "check_full_name_len"); err != nil {
		t.Fatalf("Phase 3 ValidateConstraintNonBlocking failed: %v", err)
	}

	// ==============================================================
	// Phase 4: CONTRACT — Drop deprecated columns under bounded lock
	// ==============================================================
	contractStep := core.OnlineEvolutionStep{
		Phase:         core.PhaseContract,
		Name:          "drop_status_column",
		LockTimeoutMs: 1500,
		UpSQL:         "ALTER TABLE members DROP COLUMN status;",
	}
	if err := app.ExecuteOnlineDDL(contractStep); err != nil {
		t.Fatalf("Phase 4 Contract failed: %v", err)
	}

	contractedTbl, err := app.IntrospectTable("members")
	if err != nil {
		t.Fatalf("IntrospectTable after contract failed: %v", err)
	}
	if contractedTbl.FindColumn("status") != nil {
		t.Errorf("Expected status column to be dropped after contract")
	}

	// Verify total data integrity preserved
	var totalMembers int
	err = app.DB().NewQuery("SELECT count(*) FROM members").Row(&totalMembers)
	if err != nil || totalMembers != 5 {
		t.Errorf("Expected 5 members preserved, got %d", totalMembers)
	}
}

func TestOnlineSchemaEvolution_BoundedLockTimeoutEnforcement(t *testing.T) {
	app, err := tests.NewTestApp()
	if err != nil {
		t.Fatalf("Failed to initialize test app: %v", err)
	}
	defer app.Cleanup()

	// 1. Create table
	if _, err := app.DB().NewQuery("CREATE TABLE lock_target (id text PRIMARY KEY, val int);").Execute(); err != nil {
		t.Fatalf("Failed to create lock_target: %v", err)
	}

	// 2. Open a separate transaction in background holding an ACCESS EXCLUSIVE lock
	lockAcquired := make(chan struct{})
	done := make(chan struct{})

	go func() {
		_ = app.RunInTransaction(func(txApp core.App) error {
			_, err := txApp.DB().NewQuery("LOCK TABLE lock_target IN ACCESS EXCLUSIVE MODE;").Execute()
			if err != nil {
				return err
			}
			close(lockAcquired)
			<-done
			return nil
		})
	}()

	<-lockAcquired
	defer close(done)

	// 3. Attempt to run online DDL with tight 200ms lock timeout
	step := core.OnlineEvolutionStep{
		Phase:         core.PhaseExpand,
		Name:          "contended_ddl",
		LockTimeoutMs: 200, // 200ms
		UpSQL:         "ALTER TABLE lock_target ADD COLUMN extra text;",
	}

	start := time.Now()
	err = app.ExecuteOnlineDDL(step)
	duration := time.Since(start)

	if err == nil {
		t.Fatalf("Expected ExecuteOnlineDDL to fail with lock timeout, but succeeded")
	}

	// Verify duration is bounded (approx 200ms, definitely < 2000ms)
	if duration > 2*time.Second {
		t.Errorf("Lock timeout exceeded budget: took %v", duration)
	}

	// Verify error code or message
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code != "55P03" { // lock_not_available
			t.Errorf("Expected PostgreSQL SQLSTATE 55P03 (lock_not_available), got %s", pgErr.Code)
		}
	} else if !strings.Contains(err.Error(), "timeout") && !strings.Contains(err.Error(), "lock") {
		t.Errorf("Expected lock timeout error message, got: %v", err)
	}
}
