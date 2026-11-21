---
title: "JavaScript models, jobs and migrations"
description: "Use embedded JS bindings with PostgreSQL-aware data operations and reviewed schema changes."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Model access
The embedded runtime provides $app and per-handler event bindings. It is privileged server code. Apply authorization before reading or mutating private data. Use methods exposed by the generated types.d.ts at the app's data directory; do not assume all Node.js APIs or arbitrary npm packages work.

A minimal server-side transaction pattern, after authorization:
```js
$app.runInTransaction((txApp) => {
    const record = txApp.findRecordById("posts", postId);
    record.set("title", newTitle);
    txApp.save(record);
});
```
The parameters postId and newTitle are application-validated inputs. This is illustrative, not a public route. Use txApp throughout the transaction.

## Migrations and scheduled jobs
The base executable registers migratecmd and loads pb_migrations by default; the --migrationsDir flag selects the directory. Migration files declare migrate(up, down); callbacks receive the application scoped to the migration transaction. Use generated migration templates and review the operations; apply on a clean and upgraded disposable database before release. Do not execute destructive rollback as a routine test against production.

cronAdd registers a named scheduled job using the embedded runtime. Jobs may use shared state and external services only under the established operational constraints. Do not assume exactly-once external side effects or keep critical state only in memory. Use [clustering](/ruxbase/operations/clustering/) and [SQL compatibility](/ruxbase/migration/compatibility/).

## Evidence
[Source: plugins/jsvm/jsvm.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/plugins/jsvm/jsvm.go), [Source: plugins/jsvm/binds.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/plugins/jsvm/binds.go), [Source: plugins/migratecmd/templates.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/plugins/migratecmd/templates.go). Transaction, migration and job snippets/patterns are guidance; the executable JS-route example is independently exercised in the normal server quickstart.
