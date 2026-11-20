---
title: "Collection types and schema design"
description: "Choose base, auth and view collections and understand schema lifecycle."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Purpose and supported types
A collection defines a data schema and access rules; its records are application data. The supported collection types are base, auth and view.

| Type | Use | Write behavior |
| --- | --- | --- |
| base | Ordinary application records | CRUD subject to fields and rules |
| auth | Records that can authenticate | Account and ordinary record operations with configured rules |
| view | PostgreSQL query-backed projection | Read-only records; change source collections instead |

A view query must use PostgreSQL SQL and expose a usable id. Do not use SQLite-only SQL or assume a view inherits all application authorization automatically. Review the view's own API rules, selected fields and exposure.

## Define a collection
In the local dashboard, create a base collection named posts with the fields in [the example app](/ruxbase/integrations/example-app/). Choose field constraints and list/view/create/update/delete rules before connecting a client. Record IDs and collection names are not interchangeable; a collection can be referenced by name or ID where the endpoint accepts either.

Schema mutations update PostgreSQL tables and policies. Use reviewed migration steps, bounded locks and recovery plans for live data; do not mutate the schema from an ordinary browser. Read [operations](/ruxbase/operations/overview/) and [compatibility](/ruxbase/migration/compatibility/).

## Evidence
[Source: core/collection_model.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/collection_model.go) defines the three types and constructors. The collection save/delete execution uses transactions; the JS SDK fixture provisions a base collection and uses auth records. View queries require separate tests. See [field types](/ruxbase/concepts/fields/) and [rules](/ruxbase/concepts/rules/).
