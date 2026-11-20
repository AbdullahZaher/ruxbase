package operatorprep

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/AbdullahZaher/ruxbase/core"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// SystemStatus describes only the registered system migration ledger. It is
// not a statement about schema integrity, RLS correctness or serving readiness.
type SystemStatus struct {
	Missing     []string `json:"missing"`
	Conditional []string `json:"conditional"`
}

// CheckSystemHistory reads the existing ledger without bootstrapping the app,
// running migration callbacks, or creating any database objects.
func CheckSystemHistory(ctx context.Context, dsn, expectedRole string) (SystemStatus, error) {
	if dsn == "" || expectedRole == "" {
		return SystemStatus{}, errors.New("explicit database URL and role required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return SystemStatus{}, errors.New("unable to open history inspection connection")
	}
	defer db.Close()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return SystemStatus{}, errors.New("unable to begin read-only history inspection")
	}
	defer func() { _ = tx.Rollback() }()
	var session, current string
	if err := tx.QueryRowContext(ctx, "SELECT session_user, current_user").Scan(&session, &current); err != nil ||
		session != expectedRole || current != expectedRole {
		return SystemStatus{}, errors.New("history inspection identity mismatch or unavailable")
	}

	status := SystemStatus{Missing: []string{}, Conditional: []string{}}
	var exists bool
	if err := tx.QueryRowContext(ctx, "SELECT pg_catalog.to_regclass('public._migrations') IS NOT NULL").Scan(&exists); err != nil {
		return status, errors.New("unable to locate migration history")
	}
	applied := map[string]bool{}
	if exists {
		rows, err := tx.QueryContext(ctx, "SELECT file FROM public._migrations")
		if err != nil {
			return status, errors.New("unable to read migration history")
		}
		defer rows.Close()
		for rows.Next() {
			var file string
			if err := rows.Scan(&file); err != nil {
				return status, errors.New("invalid migration history")
			}
			applied[file] = true
		}
		if err := rows.Err(); err != nil {
			return status, errors.New("incomplete migration history")
		}
	}

	if len(core.SystemMigrations.Items()) == 0 {
		return status, errors.New("system migration registry unavailable")
	}
	for _, migration := range core.SystemMigrations.Items() {
		if !applied[migration.File] {
			status.Missing = append(status.Missing, migration.File)
		}
		if migration.ReapplyCondition != nil {
			// A condition is arbitrary code. Do not execute it in the reader.
			status.Conditional = append(status.Conditional, migration.File)
		}
	}
	sort.Strings(status.Missing)
	sort.Strings(status.Conditional)

	if err := tx.Commit(); err != nil {
		return status, errors.New("unable to finish history inspection")
	}
	if !exists || len(status.Missing) != 0 || len(status.Conditional) != 0 {
		return status, errors.New("system history requires operator review")
	}
	return status, nil
}
