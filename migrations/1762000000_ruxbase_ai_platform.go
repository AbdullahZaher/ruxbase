package migrations

import (
	"github.com/AbdullahZaher/ruxbase/core"
)

// ruxbase AI agent governance, human approvals, knowledge/RAG store, and versioned plugins tables.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			-- scoped AI agent identities
			CREATE TABLE IF NOT EXISTS {{_ai_agents}} (
				[[id]]                  TEXT PRIMARY KEY NOT NULL,
				[[name]]                TEXT UNIQUE NOT NULL,
				[[owner_id]]            TEXT DEFAULT '' NOT NULL,
				[[scopes]]              JSONB,
				[[allowed_collections]] JSONB,
				[[status]]              TEXT DEFAULT 'active' NOT NULL,
				[[created]]             TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]             TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__ai_agents_owner_status}} ON {{_ai_agents}} ([[owner_id]], [[status]]);

			-- human approval workflow for sensitive/destructive agent tool calls
			CREATE TABLE IF NOT EXISTS {{_ai_tool_approvals}} (
				[[id]]              TEXT PRIMARY KEY NOT NULL,
				[[agent_id]]        TEXT NOT NULL REFERENCES {{_ai_agents}}([[id]]) ON DELETE CASCADE,
				[[tool_name]]       TEXT NOT NULL,
				[[classification]]  TEXT NOT NULL,
				[[target_resource]] TEXT DEFAULT '' NOT NULL,
				[[payload]]         JSONB,
				[[status]]          TEXT DEFAULT 'pending' NOT NULL,
				[[approved_by]]     TEXT DEFAULT '' NOT NULL,
				[[decision_reason]] TEXT DEFAULT '' NOT NULL,
				[[created]]         TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]         TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__ai_tool_approvals_agent_status}} ON {{_ai_tool_approvals}} ([[agent_id]], [[status]]);

			-- immutable AI tool permission decision and invocation audit log
			CREATE TABLE IF NOT EXISTS {{_ai_audit_logs}} (
				[[id]]              TEXT PRIMARY KEY NOT NULL,
				[[agent_id]]        TEXT DEFAULT '' NOT NULL,
				[[owner_id]]        TEXT DEFAULT '' NOT NULL,
				[[tool_name]]       TEXT NOT NULL,
				[[classification]]  TEXT NOT NULL,
				[[target_resource]] TEXT DEFAULT '' NOT NULL,
				[[decision]]        TEXT NOT NULL,
				[[reason]]          TEXT DEFAULT '' NOT NULL,
				[[created]]         TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__ai_audit_logs_agent_created}} ON {{_ai_audit_logs}} ([[agent_id]], [[created]] DESC);

			-- optional tenant-isolated knowledge/RAG chunks
			CREATE TABLE IF NOT EXISTS {{_ai_knowledge_chunks}} (
				[[id]]             TEXT PRIMARY KEY NOT NULL,
				[[namespace]]      TEXT DEFAULT 'default' NOT NULL,
				[[owner_id]]       TEXT DEFAULT '' NOT NULL,
				[[title]]          TEXT DEFAULT '' NOT NULL,
				[[content]]        TEXT NOT NULL,
				[[metadata]]       JSONB,
				[[created]]        TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]        TIMESTAMPTZ DEFAULT now() NOT NULL
			);
			CREATE INDEX IF NOT EXISTS {{idx__ai_knowledge_owner_ns}} ON {{_ai_knowledge_chunks}} ([[owner_id]], [[namespace]]);

			-- versioned plugin registry
			CREATE TABLE IF NOT EXISTS {{_plugins}} (
				[[id]]               TEXT PRIMARY KEY NOT NULL,
				[[name]]             TEXT UNIQUE NOT NULL,
				[[version]]          TEXT NOT NULL,
				[[min_core_version]] TEXT DEFAULT '0.1.0' NOT NULL,
				[[status]]           TEXT DEFAULT 'loaded' NOT NULL,
				[[permissions]]      JSONB,
				[[created]]          TIMESTAMPTZ DEFAULT now() NOT NULL,
				[[updated]]          TIMESTAMPTZ DEFAULT now() NOT NULL
			);
		`).Execute()

		return err
	}, func(txApp core.App) error {
		_, err := txApp.DB().NewQuery(`
			DROP TABLE IF EXISTS {{_plugins}};
			DROP TABLE IF EXISTS {{_ai_knowledge_chunks}};
			DROP TABLE IF EXISTS {{_ai_audit_logs}};
			DROP TABLE IF EXISTS {{_ai_tool_approvals}};
			DROP TABLE IF EXISTS {{_ai_agents}};
		`).Execute()

		return err
	})
}
