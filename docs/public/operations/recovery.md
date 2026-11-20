---
title: "Backup, restore and operational monitoring"
description: "Plan database and attachment recovery and interpret operational health signals."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Recovery prerequisites
Backups contain a PostgreSQL dump and local data files, with logs dump where configured separately. External object storage requires its own verified backup/recovery policy. Ensure pg_dump and pg_restore are installed at compatible versions; the embedded development binaries do not include them. Review retention, storage permissions and independent recovery credentials.

## Restore contract
Ruxbase currently restores a backup from the command line with ruxbase restore <backup> --yes after all instances have been stopped; the inherited restore API and dashboard do not provide a working database restore path. Rehearse in a disposable target and verify schema, row counts, authorization and stored files. Do not restore or overwrite live data without an approved recovery plan. Dumps omit server role privileges; check role/policy setup after restoring.

## Monitoring signals
Review readiness, dbPool wait counters, cluster LISTEN connectivity and realtime lag/retained WAL as applicable. Counters are per instance and reset on restart. Keep administrative diagnostics restricted. Alerts and RPO/RTO are application-specific; require actual timed recovery evidence before committing to them.

## Evidence
[Source: docs/COMPATIBILITY.md](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/docs/COMPATIBILITY.md), [Source: tests/e2e/backup_test.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/e2e/backup_test.go), [Source: docs/OPERATIONS.md](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/docs/OPERATIONS.md). See [cluster considerations](/ruxbase/operations/clustering/) and [realtime monitoring](/ruxbase/guides/realtime/).
