# W01-M01 — experimental prepared metadata bootstrap

Integrated baseline: 0f4d07bac60e32811ff45d419762a2a1b71e4de7
(tree fd0c2581482181d64739eda391ce337353b9d16b).

This increment adds an opt-in core.BaseAppConfig.PreparedRuntimeRole profile
for isolated engineering. Normal bootstrap remains unchanged. It is not
enabled by environment fallback or exposed as a production CLI serving mode.

The prepared profile initializes the existing main and auxiliary pools,
checks each sampled identity with the existing authority inspector, verifies
registered system and app migration entries and the reviewed auxiliary
condition, checks the RLS state marker and loads existing settings and
collection metadata. It never runs migration callbacks, RLS repair, settings
auto-initialization, bootstrap hooks or background cluster/logger/realtime
startup. It rejects owner/excess-authority samples, missing metadata and
unknown conditional migrations. Partial initialization closes pools and clears
published metadata. The explicit core migration methods reject the profile;
normal apis.Serve therefore rejects before opening its listener. Direct
trusted Go code and custom routers are outside this admission restriction.
This is not a process sandbox or a new authorization engine.

The fixture tests grant only metadata SELECT and schema usage to a generated
restricted role on a disposable PostgreSQL database. They verify actual
bootstrap, cached collection access, refusal of normal serving and migration
entrypoints, owner rejection, unchanged migration history and missing settings
rejection without implicit repair. No persistent or production DB is accessed.

Remaining before admitting HTTP traffic: complete application-wide grants
and pool-wide identity enforcement, safe app migration lifecycle, actual RLS
policy/catalog integrity validation, authorization coverage for all CRUD and
administrative paths, cluster/replication, hooks and logging compatibility,
health checks, performance and independent certification. Ledger and RLS
marker checks are not proof of schema integrity; these are separate snapshots,
not a concurrent authority drift guarantee. Do not use this profile for
production serving.

CI now runs on push to wave/** as well as main, preserving all existing jobs.
This allows direct evidence on the exact wave tree after integration.
