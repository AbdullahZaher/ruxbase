package core

import (
	"errors"
	"fmt"

	"github.com/pocketbase/dbx"
)

var (
	// ErrTransactionalPoolReset is returned when ResetDBPool is called on an active transaction app.
	ErrTransactionalPoolReset = errors.New("cannot reset connection pool during an active transaction")

	// ErrDBPoolClosed is returned when an operation is attempted on a closed database connection pool.
	ErrDBPoolClosed = errors.New("database connection pool is closed")
)

type poolCloser interface {
	Close() error
}

// ResetDBPool gracefully closes existing database connection pools and
// re-establishes them cleanly using the configured connectors and pool limits.
// Returns ErrTransactionalPoolReset if called on an app instance with an active transaction.
func (app *BaseApp) ResetDBPool() error {
	if app.IsTransactional() {
		return ErrTransactionalPoolReset
	}

	// Close existing pools
	if err := app.CloseDBPool(); err != nil {
		return fmt.Errorf("failed to close existing db pool: %w", err)
	}

	// Reinitialize regular and auxiliary DB pools
	if err := app.initDataDB(); err != nil {
		return fmt.Errorf("failed to reinitialize data db pool: %w", err)
	}

	if err := app.initAuxDB(); err != nil {
		return fmt.Errorf("failed to reinitialize aux db pool: %w", err)
	}

	return nil
}

// CloseDBPool closes the application's database connection pools cleanly.
func (app *BaseApp) CloseDBPool() error {
	if app.IsTransactional() {
		return ErrTransactionalPoolReset
	}

	var errs []error
	closed := map[poolCloser]struct{}{}

	dbs := []*dbx.Builder{
		&app.concurrentDB,
		&app.nonconcurrentDB,
		&app.auxConcurrentDB,
		&app.auxNonconcurrentDB,
	}

	for _, db := range dbs {
		if db == nil {
			continue
		}
		if v, ok := (*db).(poolCloser); ok {
			if _, isClosed := closed[v]; !isClosed {
				closed[v] = struct{}{}
				if err := v.Close(); err != nil {
					errs = append(errs, err)
				}
			}
		}
		*db = nil
	}
	app.dataDB = nil

	return errors.Join(errs...)
}
