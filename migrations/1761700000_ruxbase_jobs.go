package migrations

import (
	"github.com/AbdullahZaher/ruxbase/core"
)

// ruxbase durable background jobs, outbox, and webhook tables.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			-- durable background jobs table
			CREATE TABLE IF NOT EXISTS {{_jobs}} (
				[[id]]              TEXT PRIMARY KEY NOT NULL,
				[[queue]]           TEXT DEFAULT 'default' NOT NULL,
				[[task]]            TEXT NOT NULL,
				[[payload]]         JSONB,
				[[status]]          TEXT DEFAULT 'pending' NOT NULL,
				[[attempts]]        INTEGER DEFAULT 0 NOT NULL,
				[[max_retries]]     INTEGER DEFAULT 3 NOT NULL,
				[[backoff_seconds]] INTEGER DEFAULT 10 NOT NULL,
				[[idempotency_key]] TEXT DEFAULT '' NOT NULL,
				[[last_error]]      TEXT DEFAULT '' NOT NULL,
				[[result]]          JSONB,
				[[run_at]]          TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[leased_at]]       TIMESTAMPTZ,
				[[leased_until]]    TIMESTAMPTZ,
				[[leased_by]]       TEXT DEFAULT '' NOT NULL,
				[[created]]         TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]         TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE UNIQUE INDEX IF NOT EXISTS {{idx__jobs_idempotency_key}} ON {{_jobs}} ([[idempotency_key]]) WHERE [[idempotency_key]] != '';
			CREATE INDEX IF NOT EXISTS {{idx__jobs_queue_status_run_at}} ON {{_jobs}} ([[queue]], [[status]], [[run_at]]);
			CREATE INDEX IF NOT EXISTS {{idx__jobs_leased_until}} ON {{_jobs}} ([[leased_until]]);

			-- transactional outbox events table
			CREATE TABLE IF NOT EXISTS {{_outbox_events}} (
				[[id]]           TEXT PRIMARY KEY NOT NULL,
				[[event]]        TEXT NOT NULL,
				[[topic]]        TEXT DEFAULT '' NOT NULL,
				[[payload]]      JSONB,
				[[status]]       TEXT DEFAULT 'pending' NOT NULL,
				[[attempts]]     INTEGER DEFAULT 0 NOT NULL,
				[[last_error]]   TEXT DEFAULT '' NOT NULL,
				[[created]]      TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[published_at]] TIMESTAMPTZ
			);
			CREATE INDEX IF NOT EXISTS {{idx__outbox_events_status_created}} ON {{_outbox_events}} ([[status]], [[created]]);

			-- webhooks configuration table
			CREATE TABLE IF NOT EXISTS {{_webhooks}} (
				[[id]]       TEXT PRIMARY KEY NOT NULL,
				[[name]]     TEXT NOT NULL,
				[[url]]      TEXT NOT NULL,
				[[secret]]   TEXT DEFAULT '' NOT NULL,
				[[events]]   JSONB,
				[[active]]   BOOLEAN DEFAULT true NOT NULL,
				[[created]]  TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]  TIMESTAMPTZ DEFAULT now() NOT NULL
			);

			-- webhook delivery attempt audit logs
			CREATE TABLE IF NOT EXISTS {{_webhook_attempts}} (
				[[id]]            TEXT PRIMARY KEY NOT NULL,
				[[webhook_id]]    TEXT NOT NULL,
				[[event_id]]      TEXT NOT NULL,
				[[status_code]]   INTEGER DEFAULT 0 NOT NULL,
				[[response_body]] TEXT DEFAULT '' NOT NULL,
				[[error]]         TEXT DEFAULT '' NOT NULL,
				[[duration_ms]]   INTEGER DEFAULT 0 NOT NULL,
				[[created]]       TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__webhook_attempts_webhook_created}} ON {{_webhook_attempts}} ([[webhook_id]], [[created]]);

			-- workflows execution and state persistence table
			CREATE TABLE IF NOT EXISTS {{_workflows}} (
				[[id]]            TEXT PRIMARY KEY NOT NULL,
				[[name]]          TEXT NOT NULL,
				[[status]]        TEXT DEFAULT 'pending' NOT NULL,
				[[current_step]]  INTEGER DEFAULT 0 NOT NULL,
				[[steps]]         JSONB,
				[[context]]       JSONB,
				[[schedule_cron]] TEXT DEFAULT '' NOT NULL,
				[[next_run]]      TIMESTAMPTZ,
				[[last_error]]    TEXT DEFAULT '' NOT NULL,
				[[created]]       TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]       TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__workflows_status_next_run}} ON {{_workflows}} ([[status]], [[next_run]]);
		`).Execute()

		return err
	}, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			DROP TABLE IF EXISTS {{_workflows}};
			DROP TABLE IF EXISTS {{_webhook_attempts}};
			DROP TABLE IF EXISTS {{_webhooks}};
			DROP TABLE IF EXISTS {{_outbox_events}};
			DROP TABLE IF EXISTS {{_jobs}};
		`).Execute()

		return err
	})
}
