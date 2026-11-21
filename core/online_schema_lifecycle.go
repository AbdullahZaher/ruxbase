package core

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/pocketbase/dbx"
)

// OnlineEvolutionPhase represents one of the four phases of online schema evolution.
type OnlineEvolutionPhase string

const (
	PhaseExpand   OnlineEvolutionPhase = "EXPAND"
	PhaseBackfill OnlineEvolutionPhase = "BACKFILL"
	PhaseValidate OnlineEvolutionPhase = "VALIDATE"
	PhaseContract OnlineEvolutionPhase = "CONTRACT"
)

// OnlineEvolutionStep defines a single online schema evolution operation with lock budgets.
type OnlineEvolutionStep struct {
	Phase            OnlineEvolutionPhase `json:"phase"`
	Name             string               `json:"name"`
	LockTimeoutMs    int                  `json:"lockTimeoutMs"`    // Lock timeout in ms (default 2000)
	StatementTimeout int                  `json:"statementTimeout"` // Statement timeout in ms (default 10000)
	UpSQL            string               `json:"upSQL"`
	DownSQL          string               `json:"downSQL"`
}

// BackfillConfig configures chunked batch population of newly expanded columns.
type BackfillConfig struct {
	TableName    string
	PrimaryKey   string // Defaults to "id"
	SourceColumn string
	TargetColumn string
	TransformSQL string // Optional SQL expression, e.g. "UPPER(source)" or "source"
	BatchSize    int    // Number of rows per transaction batch (default 500)
}

// BackfillResult summarizes the execution metrics of chunked backfill.
type BackfillResult struct {
	TotalRowsUpdated int   `json:"totalRowsUpdated"`
	BatchesRun       int   `json:"batchesRun"`
	DurationMs       int64 `json:"durationMs"`
}

// ExecuteOnlineDDL executes an online schema evolution DDL step under bounded lock timeouts.
func (app *BaseApp) ExecuteOnlineDDL(step OnlineEvolutionStep) error {
	if strings.TrimSpace(step.UpSQL) == "" {
		return errors.New("cannot execute empty online DDL")
	}

	lockTimeout := step.LockTimeoutMs
	if lockTimeout <= 0 {
		lockTimeout = 2000 // 2 seconds default lock budget
	}

	stmtTimeout := step.StatementTimeout
	if stmtTimeout <= 0 {
		stmtTimeout = 10000 // 10 seconds default statement budget
	}

	return app.RunInTransaction(func(txApp App) error {
		// Set bounded lock timeout and statement timeout for this transaction
		timeoutSQL := fmt.Sprintf(
			"SET LOCAL lock_timeout = '%dms'; SET LOCAL statement_timeout = '%dms';",
			lockTimeout,
			stmtTimeout,
		)
		if _, err := txApp.DB().NewQuery(timeoutSQL).Execute(); err != nil {
			return fmt.Errorf("failed to set lock timeout: %w", err)
		}

		if _, err := txApp.DB().NewQuery(step.UpSQL).Execute(); err != nil {
			return fmt.Errorf("online DDL %q failed: %w", step.Name, err)
		}

		return nil
	})
}

// RevertOnlineDDL reverses an online schema evolution DDL step under bounded lock timeouts.
func (app *BaseApp) RevertOnlineDDL(step OnlineEvolutionStep) error {
	if strings.TrimSpace(step.DownSQL) == "" {
		return errors.New("cannot revert online DDL: downSQL is empty")
	}

	lockTimeout := step.LockTimeoutMs
	if lockTimeout <= 0 {
		lockTimeout = 2000
	}

	return app.RunInTransaction(func(txApp App) error {
		timeoutSQL := fmt.Sprintf("SET LOCAL lock_timeout = '%dms';", lockTimeout)
		if _, err := txApp.DB().NewQuery(timeoutSQL).Execute(); err != nil {
			return err
		}

		if _, err := txApp.DB().NewQuery(step.DownSQL).Execute(); err != nil {
			return fmt.Errorf("revert online DDL %q failed: %w", step.Name, err)
		}

		return nil
	})
}

// ExecuteChunkedBackfill populates target column values in primary-key ordered slices
// to prevent table locking or connection starvation.
func (app *BaseApp) ExecuteChunkedBackfill(cfg BackfillConfig) (*BackfillResult, error) {
	if err := ValidateExposedTableName(cfg.TableName); err != nil {
		return nil, err
	}

	pk := cfg.PrimaryKey
	if pk == "" {
		pk = "id"
	}

	batchSize := cfg.BatchSize
	if batchSize <= 0 {
		batchSize = 500
	}

	expr := cfg.TransformSQL
	if expr == "" {
		expr = fmt.Sprintf("%q", cfg.SourceColumn)
	}

	start := time.Now()
	totalUpdated := 0
	batches := 0

	var lastPK any = ""
	hasMore := true

	for hasMore {
		var rowsUpdated int64
		var batchLastPK any

		err := app.RunInTransaction(func(txApp App) error {
			// Find next slice of PKs needing backfill
			query := txApp.DB().Select(pk).
				From(cfg.TableName).
				Where(dbx.NewExp(fmt.Sprintf("%q IS NULL", cfg.TargetColumn)))

			if lastPK != "" {
				query = query.AndWhere(dbx.NewExp(fmt.Sprintf("%q > {:lastPK}", pk), dbx.Params{"lastPK": lastPK}))
			}

			var batchPKs []string
			if err := query.OrderBy(pk + " ASC").Limit(int64(batchSize)).Column(&batchPKs); err != nil {
				return err
			}

			if len(batchPKs) == 0 {
				hasMore = false
				return nil
			}

			batchLastPK = batchPKs[len(batchPKs)-1]

			// Update this batch
			updateSQL := fmt.Sprintf(
				`UPDATE %q SET %q = %s WHERE %q IN (SELECT unnest({:pks}::text[]))`,
				cfg.TableName,
				cfg.TargetColumn,
				expr,
				pk,
			)

			res, err := txApp.DB().NewQuery(updateSQL).Bind(dbx.Params{"pks": batchPKs}).Execute()
			if err != nil {
				return err
			}

			rowsUpdated, _ = res.RowsAffected()
			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("chunked backfill failed at batch %d: %w", batches+1, err)
		}

		if !hasMore {
			break
		}

		batches++
		totalUpdated += int(rowsUpdated)
		lastPK = batchLastPK

		if int(rowsUpdated) < batchSize {
			hasMore = false
		}
	}

	return &BackfillResult{
		TotalRowsUpdated: totalUpdated,
		BatchesRun:       batches,
		DurationMs:       time.Since(start).Milliseconds(),
	}, nil
}

// ValidateConstraintNonBlocking validates an existing NOT VALID constraint using
// SHARE UPDATE EXCLUSIVE lock instead of table-exclusive lock.
func (app *BaseApp) ValidateConstraintNonBlocking(tableName, constraintName string) error {
	if err := ValidateExposedTableName(tableName); err != nil {
		return err
	}

	query := fmt.Sprintf("ALTER TABLE %q VALIDATE CONSTRAINT %q", tableName, constraintName)
	if _, err := app.DB().NewQuery(query).Execute(); err != nil {
		return fmt.Errorf("failed to validate constraint %q on %q: %w", constraintName, tableName, err)
	}

	return nil
}
