package migrations

import (
	"github.com/AbdullahZaher/ruxbase/core"
)

// ruxbase storage buckets, objects, and function compute tables.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			-- storage buckets
			CREATE TABLE IF NOT EXISTS {{_storage_buckets}} (
				[[id]]                 TEXT PRIMARY KEY NOT NULL,
				[[name]]               TEXT UNIQUE NOT NULL,
				[[public]]             BOOLEAN DEFAULT false NOT NULL,
				[[file_size_limit]]    BIGINT DEFAULT 10485760 NOT NULL,
				[[allowed_mime_types]] JSONB,
				[[created]]            TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]            TIMESTAMPTZ DEFAULT now() NOT NULL
			);

			-- storage objects
			CREATE TABLE IF NOT EXISTS {{_storage_objects}} (
				[[id]]            TEXT PRIMARY KEY NOT NULL,
				[[bucket_id]]     TEXT NOT NULL REFERENCES {{_storage_buckets}}([[id]]) ON DELETE CASCADE,
				[[name]]          TEXT NOT NULL,
				[[owner_id]]      TEXT DEFAULT '' NOT NULL,
				[[size]]          BIGINT DEFAULT 0 NOT NULL,
				[[mime_type]]     TEXT DEFAULT 'application/octet-stream' NOT NULL,
				[[etag]]          TEXT DEFAULT '' NOT NULL,
				[[metadata]]      JSONB,
				[[created]]       TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]       TIMESTAMPTZ DEFAULT now() NOT NULL,
				UNIQUE([[bucket_id]], [[name]])
			);
			CREATE INDEX IF NOT EXISTS {{idx__storage_objects_bucket_owner}} ON {{_storage_objects}} ([[bucket_id]], [[owner_id]]);

			-- multipart / resumable upload sessions
			CREATE TABLE IF NOT EXISTS {{_storage_uploads}} (
				[[id]]          TEXT PRIMARY KEY NOT NULL,
				[[bucket_id]]   TEXT NOT NULL REFERENCES {{_storage_buckets}}([[id]]) ON DELETE CASCADE,
				[[name]]        TEXT NOT NULL,
				[[owner_id]]    TEXT DEFAULT '' NOT NULL,
				[[parts]]       JSONB,
				[[total_size]]  BIGINT DEFAULT 0 NOT NULL,
				[[status]]      TEXT DEFAULT 'pending' NOT NULL,
				[[created]]     TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]     TIMESTAMPTZ DEFAULT now() NOT NULL
			);

			-- isolated compute functions
			CREATE TABLE IF NOT EXISTS {{_functions}} (
				[[id]]          TEXT PRIMARY KEY NOT NULL,
				[[name]]        TEXT UNIQUE NOT NULL,
				[[entrypoint]]  TEXT NOT NULL,
				[[runtime]]     TEXT DEFAULT 'js' NOT NULL,
				[[code]]        TEXT NOT NULL,
				[[env_vars]]    JSONB,
				[[timeout_sec]] INTEGER DEFAULT 10 NOT NULL,
				[[status]]      TEXT DEFAULT 'active' NOT NULL,
				[[version]]     INTEGER DEFAULT 1 NOT NULL,
				[[created]]     TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]     TIMESTAMPTZ DEFAULT now() NOT NULL
			);

			-- function invocation audit logs
			CREATE TABLE IF NOT EXISTS {{_function_invocations}} (
				[[id]]          TEXT PRIMARY KEY NOT NULL,
				[[function_id]] TEXT NOT NULL REFERENCES {{_functions}}([[id]]) ON DELETE CASCADE,
				[[status]]      TEXT NOT NULL,
				[[duration_ms]] INTEGER DEFAULT 0 NOT NULL,
				[[error]]       TEXT DEFAULT '' NOT NULL,
				[[created]]     TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__function_invocations_func_created}} ON {{_function_invocations}} ([[function_id]], [[created]]);
		`).Execute()

		return err
	}, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			DROP TABLE IF EXISTS {{_function_invocations}};
			DROP TABLE IF EXISTS {{_functions}};
			DROP TABLE IF EXISTS {{_storage_uploads}};
			DROP TABLE IF EXISTS {{_storage_objects}};
			DROP TABLE IF EXISTS {{_storage_buckets}};
		`).Execute()

		return err
	})
}
