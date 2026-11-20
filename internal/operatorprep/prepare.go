// Package operatorprep contains the explicit, offline system preparation path.
// It does not start the HTTP server or execute user application migrations.
package operatorprep

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/AbdullahZaher/ruxbase/core"
	"github.com/pocketbase/dbx"

	_ "github.com/AbdullahZaher/ruxbase/migrations"
)

// Config intentionally has no fallback to the serving process database URL.
type Config struct {
	DatabaseURL     string
	LogsDatabaseURL string
	ExpectedRole    string
	DataDir         string
	EncryptionEnv   string
	// Require an independently preprovisioned, non-administrative migration role.
	PreprovisionedRLSRoles bool
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" ||
		strings.TrimSpace(c.ExpectedRole) == "" ||
		strings.TrimSpace(c.DataDir) == "" {
		return errors.New("explicit operator database URL, role and data directory are required")
	}
	return nil
}

// Prepare applies the existing system bootstrap in a separate operator process.
// It requires an authorized offline database. The caller is responsible for
// stopping serving nodes, backup, and reviewed migration authorization.
// This is a transitional path: legacy serving bootstrap is not yet separated.
func Prepare(c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	logsURL := c.LogsDatabaseURL
	if logsURL == "" {
		logsURL = c.DatabaseURL
	}

	// Verify the actual connection identity before admitting each bootstrap
	// pool, including the auxiliary pool. Never print DSNs or driver errors.
	var pools []*dbx.DB
	defer func() {
		for _, db := range pools {
			_ = db.Close()
		}
	}()
	connect := func(dsn string) (*dbx.DB, error) {
		db, err := core.DefaultDBConnect(dsn)
		if err != nil {
			return nil, errors.New("unable to open operator database")
		}
		pools = append(pools, db)

		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var session, current string
		err = db.DB().QueryRowContext(ctx, "SELECT session_user, current_user").Scan(&session, &current)
		if err != nil || session != c.ExpectedRole || current != c.ExpectedRole {
			return nil, errors.New("operator connection identity mismatch or unavailable")
		}
		return db, nil
	}

	app := core.NewBaseApp(core.BaseAppConfig{
		DBConnect:              connect,
		DatabaseURL:            c.DatabaseURL,
		LogsDatabaseURL:        logsURL,
		DataDir:                c.DataDir,
		EncryptionEnv:          c.EncryptionEnv,
		RealtimeSource:         "hooks",
		PreprovisionedRLSRoles: c.PreprovisionedRLSRoles,
	})
	if err := app.Bootstrap(); err != nil {
		_ = app.ClearBootstrap()
		return errors.New("operator system bootstrap failed")
	}
	if err := app.ClearBootstrap(); err != nil {
		return errors.New("operator bootstrap cleanup failed")
	}
	return nil
}
