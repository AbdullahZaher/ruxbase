package migrations

import (
	"github.com/AbdullahZaher/ruxbase/core"
)

// ruxbase multipart upload session-bound crash recovery, lease fencing, and durable orphan cleanup journal.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			ALTER TABLE {{_storage_uploads}} ADD COLUMN IF NOT EXISTS [[completion_token]] TEXT DEFAULT '' NOT NULL;
			ALTER TABLE {{_storage_uploads}} ADD COLUMN IF NOT EXISTS [[completion_leased_until]] TIMESTAMPTZ;
			ALTER TABLE {{_storage_uploads}} ADD COLUMN IF NOT EXISTS [[expected_sha256]] TEXT DEFAULT '' NOT NULL;
			ALTER TABLE {{_storage_uploads}} ADD COLUMN IF NOT EXISTS [[completed_object_id]] TEXT DEFAULT '' NOT NULL;
			CREATE INDEX IF NOT EXISTS {{idx__storage_uploads_status_updated}} ON {{_storage_uploads}} ([[status]], [[updated]]);
			CREATE UNIQUE INDEX IF NOT EXISTS {{idx__storage_uploads_completing_target}} ON {{_storage_uploads}} ([[bucket_id]], [[name]]) WHERE [[status]] = 'completing';

			ALTER TABLE {{_storage_objects}} ADD COLUMN IF NOT EXISTS [[upload_id]] TEXT DEFAULT '' NOT NULL;
			ALTER TABLE {{_storage_objects}} ADD COLUMN IF NOT EXISTS [[completion_token]] TEXT DEFAULT '' NOT NULL;

			CREATE TABLE IF NOT EXISTS {{_storage_upload_cleanup}} (
				[[id]]          TEXT PRIMARY KEY NOT NULL,
				[[upload_id]]   TEXT NOT NULL,
				[[storage_key]] TEXT NOT NULL,
				[[reason]]      TEXT DEFAULT '' NOT NULL,
				[[attempts]]    INTEGER DEFAULT 0 NOT NULL,
				[[last_error]]  TEXT DEFAULT '' NOT NULL,
				[[created]]     TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]     TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE UNIQUE INDEX IF NOT EXISTS {{idx__storage_upload_cleanup_key}} ON {{_storage_upload_cleanup}} ([[storage_key]]);
			CREATE INDEX IF NOT EXISTS {{idx__storage_upload_cleanup_upload_id}} ON {{_storage_upload_cleanup}} ([[upload_id]]);
		`).Execute()
		return err
	}, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			DROP TABLE IF EXISTS {{_storage_upload_cleanup}};
			ALTER TABLE {{_storage_objects}} DROP COLUMN IF EXISTS [[completion_token]];
			ALTER TABLE {{_storage_objects}} DROP COLUMN IF EXISTS [[upload_id]];
			DROP INDEX IF EXISTS {{idx__storage_uploads_completing_target}};
			DROP INDEX IF EXISTS {{idx__storage_uploads_status_updated}};
			ALTER TABLE {{_storage_uploads}} DROP COLUMN IF EXISTS [[completed_object_id]];
			ALTER TABLE {{_storage_uploads}} DROP COLUMN IF EXISTS [[expected_sha256]];
			ALTER TABLE {{_storage_uploads}} DROP COLUMN IF EXISTS [[completion_leased_until]];
			ALTER TABLE {{_storage_uploads}} DROP COLUMN IF EXISTS [[completion_token]];
		`).Execute()
		return err
	})
}
