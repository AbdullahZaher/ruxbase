package migrations

import (
	"github.com/AbdullahZaher/ruxbase/core"
)

// ruxbase administrative audit ledger and optional multi-project control plane inventory.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			-- immutable administrative audit log table
			CREATE TABLE IF NOT EXISTS {{_admin_audit_logs}} (
				[[id]]          TEXT PRIMARY KEY NOT NULL,
				[[actor_id]]    TEXT DEFAULT '' NOT NULL,
				[[actor_email]] TEXT DEFAULT '' NOT NULL,
				[[action]]      TEXT NOT NULL,
				[[resource]]    TEXT NOT NULL,
				[[target_id]]   TEXT DEFAULT '' NOT NULL,
				[[details]]     JSONB,
				[[ip_address]]  TEXT DEFAULT '' NOT NULL,
				[[created]]     TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__admin_audit_logs_created}} ON {{_admin_audit_logs}} ([[created]] DESC);
			CREATE INDEX IF NOT EXISTS {{idx__admin_audit_logs_resource}} ON {{_admin_audit_logs}} ([[resource]], [[action]]);

			-- optional self-hosted multi-project control plane inventory
			CREATE TABLE IF NOT EXISTS {{_control_plane_projects}} (
				[[id]]                  TEXT PRIMARY KEY NOT NULL,
				[[slug]]                TEXT UNIQUE NOT NULL,
				[[name]]                TEXT NOT NULL,
				[[environment]]         TEXT DEFAULT 'development' NOT NULL,
				[[db_name]]             TEXT UNIQUE NOT NULL,
				[[db_role]]             TEXT UNIQUE NOT NULL,
				[[status]]              TEXT DEFAULT 'active' NOT NULL,
				[[storage_quota_bytes]] BIGINT DEFAULT 1073741824 NOT NULL,
				[[max_connections]]     INTEGER DEFAULT 20 NOT NULL,
				[[created]]             TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]             TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__cp_projects_env_status}} ON {{_control_plane_projects}} ([[environment]], [[status]]);
		`).Execute()

		return err
	}, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			DROP TABLE IF EXISTS {{_control_plane_projects}};
			DROP TABLE IF EXISTS {{_admin_audit_logs}};
		`).Execute()

		return err
	})
}
