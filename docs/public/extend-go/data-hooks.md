---
title: "Go data, hooks and transactions"
description: "Use the App interface and transaction-scoped model operations on PostgreSQL."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Model APIs
Server-side code has privileged application authority; apply the request's authorization before operating on private records. Use App.FindCollectionByNameOrId, App.FindRecordById, core.NewRecord, record.Set and App.Save. A service function can update a model in a transaction:
```go
func renamePost(app core.App, id string, title string) error {
    return app.RunInTransaction(func(txApp core.App) error {
        record, err := txApp.FindRecordById("posts", id)
        if err != nil {
            return err
        }
        record.Set("title", title)
        return txApp.Save(record)
    })
}
```
This is an illustrative server-only function after authorization, not a publicly exposed handler. Use the callback's txApp for all database operations so they share the transaction. App.DB uses PostgreSQL; use bound dbx values and validated identifiers, never concatenate user input.

## Hooks, jobs, migrations
Use model hooks for model saves and request hooks for HTTP-specific context. Avoid irreversible side effects before commit. Jobs in clustered deployments have coordination constraints; see [cluster behavior](/ruxbase/operations/clustering/). Keep migrations reviewed and reversible where feasible; inspect data loss risk and rehearse on a disposable clone. Do not edit migrations already applied to a deployed database.

## Evidence
[Source: core/app.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/app.go), [Source: core/record_model.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/record_model.go), [Source: core/migrations_runner.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/migrations_runner.go). The transaction snippet is source-aligned, not independently executed as an authorization or migration certification. [PostgreSQL differences](/ruxbase/migration/compatibility/).
