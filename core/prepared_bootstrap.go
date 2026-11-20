package core

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/internal/pgrolecheck"
	"github.com/AbdullahZaher/ruxbase/tools/types"
	"github.com/pocketbase/dbx"
)

// ErrPreparedBootstrapOnly identifies the experimental metadata-only
// startup profile. HTTP serving and migration execution are not admitted.
var ErrPreparedBootstrapOnly = errors.New("prepared bootstrap profile does not authorize serving or migrations")

// bootstrapPrepared loads already-prepared metadata using restricted database
// connections. It deliberately does not execute bootstrap hooks, migrations,
// RLS repair, logger workers, cluster coordination or realtime initialization.
// The profile must remain isolated from HTTP serving until the full runtime
// authority and authorization contracts are implemented.
func (app *BaseApp) bootstrapPrepared() (err error) {
	if app.IsTransactional() || app.config.ClusterMode || app.config.IsDev ||
		(app.config.RealtimeSource != "" && app.config.RealtimeSource != "hooks") {
		return errors.New("prepared bootstrap requires an isolated non-dev hooks-only app")
	}
	if err := app.clearPreparedBootstrap(); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			err = errors.Join(err, app.clearPreparedBootstrap())
		}
	}()

	// Prepared metadata always uses the guarded connector, not an arbitrary
	// application DBConnect override. Keep the legacy connector unchanged.
	logsURL := app.config.LogsDatabaseURL
	if logsURL == "" {
		logsURL = app.config.DatabaseURL
	}
	mainDB, err := pgrolecheck.OpenGuardedPool(app.config.DatabaseURL, app.config.PreparedRuntimeRole, "public")
	if err != nil {
		return err
	}
	mainBuilder := dbx.NewFromDB(mainDB, "pgx")
	app.dataDB = mainBuilder
	app.concurrentDB = mainBuilder
	app.nonconcurrentDB = mainBuilder
	mainDB.SetMaxOpenConns(app.config.DataMaxOpenConns)
	mainDB.SetMaxIdleConns(app.config.DataMaxIdleConns)
	mainDB.SetConnMaxIdleTime(3 * time.Minute)
	mainDB.SetConnMaxLifetime(app.config.ConnMaxLifetime)

	auxDB, err := pgrolecheck.OpenGuardedPool(logsURL, app.config.PreparedRuntimeRole, AuxSchema)
	if err != nil {
		return err
	}
	auxBuilder := dbx.NewFromDB(auxDB, "pgx")
	app.auxConcurrentDB = auxBuilder
	app.auxNonconcurrentDB = auxBuilder
	auxDB.SetMaxOpenConns(app.config.AuxMaxOpenConns)
	auxDB.SetMaxIdleConns(app.config.AuxMaxIdleConns)
	auxDB.SetConnMaxIdleTime(3 * time.Minute)
	auxDB.SetConnMaxLifetime(app.config.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	main := mainDB
	aux := auxDB
	for _, db := range []*sql.DB{main, aux} {
		report, err := pgrolecheck.Inspect(ctx, db)
		if err != nil || report.SessionRole != app.config.PreparedRuntimeRole ||
			report.CurrentRole != app.config.PreparedRuntimeRole || len(report.Findings()) != 0 {
			return errors.New("prepared bootstrap runtime authority rejected")
		}
	}

	raw, err := readPreparedMetadata(ctx, main, aux, app.config.PreparedRuntimeRole)
	if err != nil {
		return err
	}
	if err := app.Settings().loadParam(app, &Param{Value: types.JSONRaw(raw)}); err != nil {
		return errors.New("prepared settings cannot be loaded")
	}
	if err := app.ReloadCachedCollections(); err != nil {
		return errors.New("prepared collection metadata cannot be loaded")
	}
	return nil
}

func readPreparedMetadata(ctx context.Context, main, aux *sql.DB, role string) (string, error) {
	if len(SystemMigrations.Items()) == 0 {
		return "", errors.New("system migration registry unavailable")
	}
	tx, err := main.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return "", errors.New("unable to begin prepared metadata inspection")
	}
	defer func() { _ = tx.Rollback() }()
	var session, current string
	if err := tx.QueryRowContext(ctx, "SELECT session_user, current_user").Scan(&session, &current); err != nil ||
		session != role || current != role {
		return "", errors.New("prepared metadata identity mismatch")
	}

	for index, registry := range []MigrationsList{SystemMigrations, AppMigrations} {
		for _, migration := range registry.Items() {
			var applied bool
			if err := tx.QueryRowContext(ctx,
				"SELECT EXISTS (SELECT 1 FROM public._migrations WHERE file = $1)",
				migration.File).Scan(&applied); err != nil || !applied {
				return "", errors.New("prepared migration history is missing or unavailable")
			}
			if migration.ReapplyCondition != nil {
				// Recognize only the reviewed built-in auxiliary condition.
				if index != 0 || migration.File != "1640988000_aux_init.go" {
					return "", errors.New("prepared migration condition requires operator review")
				}
			}
		}
	}

	var settings, state string
	if err := tx.QueryRowContext(ctx, "SELECT value FROM public._params WHERE id = $1", paramsKeySettings).Scan(&settings); err != nil {
		return "", errors.New("prepared settings are missing or unavailable")
	}
	if err := tx.QueryRowContext(ctx, "SELECT value FROM public._params WHERE id = $1", rlsStateParamKey).Scan(&state); err != nil {
		return "", errors.New("prepared RLS state is missing or unavailable")
	}
	var rlsState map[string]string
	if err := json.Unmarshal([]byte(state), &rlsState); err != nil || rlsState == nil {
		return "", errors.New("prepared RLS state is invalid")
	}
	for _, hash := range rlsState {
		if hash == "" || strings.HasPrefix(hash, rlsStateErrorPrefix) {
			return "", errors.New("prepared RLS state requires operator repair")
		}
	}
	var installed bool
	if err := tx.QueryRowContext(ctx, `
SELECT EXISTS (
    SELECT 1 FROM pg_catalog.pg_proc p
    JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = 'ruxbase_rls' AND p.proname = 'auth' AND p.pronargs = 0
)
`).Scan(&installed); err != nil || !installed {
		return "", errors.New("prepared RLS functions are missing or unavailable")
	}
	if err := tx.Commit(); err != nil {
		return "", errors.New("unable to finish prepared metadata inspection")
	}

	auxTx, err := aux.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return "", errors.New("unable to begin prepared auxiliary inspection")
	}
	defer func() { _ = auxTx.Rollback() }()
	var logsReady bool
	if err := auxTx.QueryRowContext(ctx, `
SELECT session_user, current_user,
       EXISTS (
           SELECT 1 FROM pg_catalog.pg_class c
           JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
           WHERE n.nspname = '_aux' AND c.relname = '_logs'
             AND c.relkind IN ('r', 'p')
             AND pg_catalog.has_schema_privilege(current_user, n.oid, 'USAGE')
       )
`).Scan(&session, &current, &logsReady); err != nil ||
		session != role || current != role || !logsReady {
		return "", errors.New("prepared auxiliary state is missing or unavailable")
	}
	if err := auxTx.Commit(); err != nil {
		return "", errors.New("unable to finish prepared auxiliary inspection")
	}
	return settings, nil
}

// clearPreparedBootstrap also handles partially initialized pools. No
// legacy lifecycle hooks or background workers are invoked in this profile.
func (app *BaseApp) clearPreparedBootstrap() error {
	var errs []error
	closed := map[*dbx.DB]bool{}
	for _, builder := range []dbx.Builder{app.dataDB, app.auxConcurrentDB} {
		if db, ok := builder.(*dbx.DB); ok && !closed[db] {
			closed[db] = true
			if err := db.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	app.dataDB = nil
	app.concurrentDB = nil
	app.nonconcurrentDB = nil
	app.auxConcurrentDB = nil
	app.auxNonconcurrentDB = nil
	app.settings = newDefaultSettings()
	app.Store().Set(StoreKeyCachedCollections, []*Collection(nil))
	return errors.Join(errs...)
}
