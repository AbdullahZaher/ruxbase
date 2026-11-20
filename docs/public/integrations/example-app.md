---
title: Example app: collections and rules
description: Create an example posts collection with owner-controlled access and public published records.
sidebar:
  order: 2
---

**Development documentation.** Run examples against a disposable Ruxbase instance. Never embed a database connection string or a superuser credential in a client application.

## Prerequisites

Start the disposable development instance and open its administration dashboard. Use the default users auth collection, or create an equivalent auth collection and adapt the examples. Create two ordinary users for validation. Enable password authentication for this exercise; do not weaken registration or authentication rules for real applications.

## Define the data model

Create a base collection called posts with the following fields using the dashboard. The SDK test provisions an equivalent uniquely named collection.

| Field | Type | Configuration |
| --- | --- | --- |
| title | Text | Required |
| published | Bool | Default false |
| views | Number | Default zero |
| meta | JSON | Optional |
| author | Relation | Target users, single selection |
| cover | File | Single file, at most 1 MiB |
| created | Autodate | On create |
| updated | Autodate | On create and update |

## Configure collection API rules

These rules are for the example content application only, not a universal security policy. Configure them as ordinary record API rules, not PostgreSQL SQL statements.

| Rule | Expression |
| --- | --- |
| List | `published = true || author = @request.auth.id` |
| View | `published = true || author = @request.auth.id` |
| Create | `@request.auth.id != '' && author = @request.auth.id` |
| Update | `author = @request.auth.id` |
| Delete | `author = @request.auth.id` |

A locked rule (null) is not the same as an empty rule. Keep administrative access separate. The example allows the owner to edit a record, including its author field if writable under the configured schema. If ownership transfers must be forbidden, design and test an explicit field-change restriction before deploying; these example rules are not a hardened multi-tenant policy.

## Validate behavior

| Actor | Expected |
| --- | --- |
| Guest | List published posts; cannot create |
| Owner | Create owned posts and read own drafts |
| Another authenticated user | Cannot read another user's draft or update/delete the other's post |
| Another authenticated user | Can read published posts subject to list/view rules |
| Relation expansion | Related auth records still obey their own collection rules |

Use a second account to verify denied operations. A client filter narrows results; it is not an authorization rule. See [JavaScript integration](/ruxbase/integrations/javascript/) and [REST integration](/ruxbase/integrations/rest/).

## Evidence

The model, filter rules and ordinary-user access cases are based on [Ruxbase JS SDK test suite](https://github.com/AbdullahZaher/ruxbase/blob/d03ba38b8fa2063cd1479c9198c44de21a16de93/tests/sdk/js/sdk.test.js). Additional production policies, field immutability, registration controls and application-specific data validation require their own tests. See [RLS scope](/ruxbase/authorization/row-level-security/).
