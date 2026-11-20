package cluster

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// DedicatedReplication identifies a separately provisioned WAL login.
// It is opt-in. The SQL pool and listener continue to use the app identity.
type DedicatedReplication struct {
	DatabaseURL string
	Role        string
}

func (d DedicatedReplication) Enabled() bool {
	return d.DatabaseURL != "" || d.Role != ""
}

// validate compares configured endpoints and enforces distinct login names.
// Endpoint equality is not a cryptographic cluster identity guarantee.
func (d DedicatedReplication) validate(appDSN string) (*pgx.ConnConfig, error) {
	if d.DatabaseURL == "" || d.Role == "" {
		return nil, errors.New("explicit replication database URL and role required")
	}
	app, err := pgx.ParseConfig(appDSN)
	if err != nil {
		return nil, errors.New("invalid primary database configuration")
	}
	replica, err := pgx.ParseConfig(d.DatabaseURL)
	if err != nil {
		return nil, errors.New("invalid dedicated replication configuration")
	}
	if replica.User != d.Role || replica.User == app.User ||
		replica.Database == "" || replica.Database != app.Database ||
		replica.Host != app.Host || replica.Port != app.Port {
		return nil, errors.New("dedicated replication requires a distinct role on the primary database endpoint")
	}
	for _, fallback := range replica.Fallbacks {
		if fallback.Host != replica.Host || fallback.Port != replica.Port {
			return nil, errors.New("alternate dedicated replication endpoints are unsupported")
		}
	}
	if replica.RuntimeParams["replication"] != "" {
		return nil, errors.New("dedicated replication connection must use ordinary SQL configuration")
	}
	return replica, nil
}

// check verifies the dedicated principal using an ordinary SQL connection
// before attempting WAL streaming. Never return driver errors containing DSNs.
func (d DedicatedReplication) check(ctx context.Context, appDSN string) (*pgx.ConnConfig, error) {
	config, err := d.validate(appDSN)
	if err != nil {
		return nil, err
	}
	conn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, errors.New("dedicated replication identity unavailable")
	}
	defer closePgx(conn)

	var ready bool
	err = conn.QueryRow(ctx, `
SELECT session_user = $1 AND current_user = $1
   AND current_database() = $2
   AND current_setting('wal_level') = 'logical'
   AND EXISTS (
       SELECT 1 FROM pg_catalog.pg_roles r
       WHERE r.rolname = current_user AND r.rolreplication
         AND r.rolcanlogin
         AND NOT (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolbypassrls)
   )
   AND NOT EXISTS (
       SELECT 1 FROM pg_catalog.pg_roles r
       WHERE r.rolname <> current_user AND pg_catalog.pg_has_role(current_user, r.oid, 'MEMBER')
         AND (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolbypassrls)
   )
`, d.Role, config.Database).Scan(&ready)
	if err != nil || !ready {
		return nil, errors.New("dedicated replication authority rejected")
	}
	return config, nil
}
