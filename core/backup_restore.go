package core

import (
	"context"
)

// RestoreBackup is not supported by ruxbase and always returns [ErrRestoreNotSupported].
//
// PocketBase replaces the pb_data directory and restarts the process, but
// restoring the Postgres database of a running app (or of a cluster with
// other instances using it) is not safe. Stop the app and use the
// "ruxbase restore <backup>" command (see [RestoreBackupArchive]) instead.
func (app *BaseApp) RestoreBackup(ctx context.Context, name string) error {
	// ruxbase: the in-place restore is replaced by the "ruxbase restore" command
	return ErrRestoreNotSupported
}
