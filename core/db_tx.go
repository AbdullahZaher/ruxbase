package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pocketbase/dbx"
)

var savepointSeq uint64

// RunInTransaction wraps fn into a transaction for the regular app database.
//
// It is safe to nest RunInTransaction calls as long as you use the callback's txApp.
func (app *BaseApp) RunInTransaction(fn func(txApp App) error) error {
	return app.RunInTransactionWithContext(context.Background(), fn)
}

// RunInTransactionWithContext wraps fn into a transaction for the regular app database
// with explicit context and cancellation boundary support.
//
// It is safe to nest RunInTransactionWithContext calls as long as you use the callback's txApp.
func (app *BaseApp) RunInTransactionWithContext(ctx context.Context, fn func(txApp App) error) error {
	return app.runInTransactionWithContext(ctx, app.NonconcurrentDB(), fn, false)
}

// AuxRunInTransaction wraps fn into a transaction for the auxiliary app database.
//
// It is safe to nest RunInTransaction calls as long as you use the callback's txApp.
func (app *BaseApp) AuxRunInTransaction(fn func(txApp App) error) error {
	return app.AuxRunInTransactionWithContext(context.Background(), fn)
}

// AuxRunInTransactionWithContext wraps fn into a transaction for the auxiliary app database
// with explicit context and cancellation boundary support.
//
// It is safe to nest AuxRunInTransactionWithContext calls as long as you use the callback's txApp.
func (app *BaseApp) AuxRunInTransactionWithContext(ctx context.Context, fn func(txApp App) error) error {
	return app.runInTransactionWithContext(ctx, app.AuxNonconcurrentDB(), fn, true)
}

func (app *BaseApp) runInTransactionWithContext(ctx context.Context, db dbx.Builder, fn func(txApp App) error, isForAuxDB bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	switch txOrDB := db.(type) {
	case *dbx.Tx:
		// Nested transaction: isolate child failure using a PostgreSQL SAVEPOINT
		seq := atomic.AddUint64(&savepointSeq, 1)
		spName := fmt.Sprintf("rb_sp_%d_%d", time.Now().UnixNano(), seq)

		if _, err := txOrDB.NewQuery("SAVEPOINT " + spName).WithContext(ctx).Execute(); err != nil {
			return fmt.Errorf("failed to create savepoint %s: %w", spName, err)
		}

		childTxApp := app.createNestedTxApp(txOrDB, isForAuxDB)

		fnErr := fn(childTxApp)

		if fnErr != nil {
			// Rollback to savepoint and release savepoint so outer transaction remains valid
			_, _ = txOrDB.NewQuery("ROLLBACK TO SAVEPOINT " + spName).WithContext(context.Background()).Execute()
			_, _ = txOrDB.NewQuery("RELEASE SAVEPOINT " + spName).WithContext(context.Background()).Execute()

			if childTxApp.txInfo != nil {
				_ = childTxApp.txInfo.runAfterFuncs(fnErr)
			}
			return fnErr
		}

		// Success: release savepoint
		if _, err := txOrDB.NewQuery("RELEASE SAVEPOINT " + spName).WithContext(ctx).Execute(); err != nil {
			return fmt.Errorf("failed to release savepoint %s: %w", spName, err)
		}

		// Promote child afterFuncs to parent transaction so they execute on root commit
		if childTxApp.txInfo != nil && app.txInfo != nil {
			app.txInfo.mu.Lock()
			childTxApp.txInfo.mu.Lock()
			app.txInfo.afterFuncs = append(app.txInfo.afterFuncs, childTxApp.txInfo.afterFuncs...)
			childTxApp.txInfo.afterFuncs = nil
			childTxApp.txInfo.mu.Unlock()
			app.txInfo.mu.Unlock()
		}

		return nil

	case *dbx.DB:
		var txApp *BaseApp
		txErr := txOrDB.TransactionalContext(ctx, nil, func(tx *dbx.Tx) error {
			txApp = app.createTxApp(tx, isForAuxDB)
			return fn(txApp)
		})

		// execute all after event calls on transaction complete
		if txApp != nil && txApp.txInfo != nil {
			afterFuncErr := txApp.txInfo.runAfterFuncs(txErr)
			if afterFuncErr != nil {
				return errors.Join(txErr, afterFuncErr)
			}
		}

		return txErr

	default:
		return errors.New("failed to start transaction (unknown db type)")
	}
}

// rootNonTransactionalApp returns the root non-transactional application instance.
func (app *BaseApp) rootNonTransactionalApp() *BaseApp {
	root := app
	for root.txInfo != nil && root.txInfo.parent != nil {
		root = root.txInfo.parent
	}
	return root
}

// createTxApp shallow clones the current app and assigns a new tx state.
func (app *BaseApp) createTxApp(tx *dbx.Tx, isForAuxDB bool) *BaseApp {
	clone := *app

	if isForAuxDB {
		clone.auxConcurrentDB = tx
		clone.auxNonconcurrentDB = tx
	} else {
		clone.concurrentDB = tx
		clone.nonconcurrentDB = tx
	}

	clone.txInfo = &TxAppInfo{
		parent:     app.rootNonTransactionalApp(),
		isForAuxDB: isForAuxDB,
	}

	return &clone
}

// createNestedTxApp shallow clones the current app for a nested transaction.
func (app *BaseApp) createNestedTxApp(tx *dbx.Tx, isForAuxDB bool) *BaseApp {
	clone := *app

	if isForAuxDB {
		clone.auxConcurrentDB = tx
		clone.auxNonconcurrentDB = tx
	} else {
		clone.concurrentDB = tx
		clone.nonconcurrentDB = tx
	}

	clone.txInfo = &TxAppInfo{
		parent:     app.rootNonTransactionalApp(),
		isForAuxDB: isForAuxDB,
	}

	return &clone
}

// TxAppInfo represents an active transaction context associated to an existing app instance.
type TxAppInfo struct {
	parent     *BaseApp
	afterFuncs []func(txErr error) error
	mu         sync.Mutex
	isForAuxDB bool
}

// OnComplete registers the provided callback that will be invoked
// once the related transaction ends (either completes successfully or rollbacked with an error).
//
// The callback receives the transaction error (if any) as its argument.
// Any additional errors returned by the OnComplete callbacks will be
// joined together with txErr when returning the final transaction result.
func (a *TxAppInfo) OnComplete(fn func(txErr error) error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.afterFuncs = append(a.afterFuncs, fn)
}

// note: can be called only once because TxAppInfo is cleared
func (a *TxAppInfo) runAfterFuncs(txErr error) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	var errs []error

	for _, call := range a.afterFuncs {
		if err := call(txErr); err != nil {
			errs = append(errs, err)
		}
	}

	a.afterFuncs = nil

	if len(errs) > 0 {
		return fmt.Errorf("transaction afterFunc errors: %w", errors.Join(errs...))
	}

	return nil
}
