package pgrolecheck

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// OpenGuardedPool is for the experimental metadata-only profile. Each new
// physical pgx connection and each reused connection is screened before use.
// This does not establish a complete grant contract or guard statements
// inside a checked-out connection against concurrent authority changes.
func OpenGuardedPool(dsn, role, schema string) (*sql.DB, error) {
	if dsn == "" || role == "" || (schema != "public" && schema != "_aux") {
		return nil, errors.New("explicit guarded connection configuration required")
	}
	config, err := pgx.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("invalid guarded connection configuration")
	}
	if config.User != role || config.Database == "" {
		return nil, errors.New("guarded connection configuration identity mismatch")
	}
	config.RuntimeParams["search_path"] = schema
	check := func(ctx context.Context, conn *pgx.Conn) error {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var report Report
		var database, path string
		err := conn.QueryRow(ctx, guardedAuthorityQuery).Scan(
			&report.SessionRole, &report.CurrentRole, &database, &path, &report.PrivilegedMember,
			&report.DatabaseOwner, &report.DatabaseCreate, &report.SchemaOwner,
			&report.SchemaCreate, &report.RelationOwner, &report.RoutineOwner,
		)
		if err != nil || report.SessionRole != role || report.CurrentRole != role || len(report.Findings()) != 0 {
			return errors.New("guarded connection authority rejected")
		}
		if database != config.Database || path != schema {
			return errors.New("guarded connection database or schema context rejected")
		}
		return nil
	}

	connector := stdlib.GetConnector(*config,
		stdlib.OptionAfterConnect(check),
		stdlib.OptionResetSession(func(ctx context.Context, conn *pgx.Conn) error {
			if err := check(ctx, conn); err != nil {
				// database/sql must discard this connection, not reuse it.
				return driver.ErrBadConn
			}
			return nil
		}),
	)
	return sql.OpenDB(connector), nil
}
