package migrations

import (
	"github.com/AbdullahZaher/ruxbase/core"
)

// ruxbase transactional outbox lease ownership and fencing tokens for concurrency safety.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			ALTER TABLE {{_outbox_events}} ADD COLUMN IF NOT EXISTS [[claim_token]] TEXT DEFAULT '' NOT NULL;
			ALTER TABLE {{_outbox_events}} ADD COLUMN IF NOT EXISTS [[leased_at]] TIMESTAMPTZ;
			ALTER TABLE {{_outbox_events}} ADD COLUMN IF NOT EXISTS [[leased_until]] TIMESTAMPTZ;
			CREATE INDEX IF NOT EXISTS {{idx__outbox_events_status_leased_until}} ON {{_outbox_events}} ([[status]], [[leased_until]]);
		`).Execute()
		return err
	}, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			DROP INDEX IF EXISTS {{idx__outbox_events_status_leased_until}};
			ALTER TABLE {{_outbox_events}} DROP COLUMN IF EXISTS [[leased_until]];
			ALTER TABLE {{_outbox_events}} DROP COLUMN IF EXISTS [[leased_at]];
			ALTER TABLE {{_outbox_events}} DROP COLUMN IF EXISTS [[claim_token]];
		`).Execute()
		return err
	})
}
