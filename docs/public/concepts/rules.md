---
title: "API rules and application filters"
description: "Separate access authorization from query filters and validate permitted and denied requests."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Rule semantics
An API rule is a server-side condition attached to a collection operation. A locked rule (null) restricts that operation to superusers; an empty rule permits it without an additional rule constraint, subject to endpoint validation. Record list rules also filter visible results.

The example posts app restricts writes by owner and exposes published records according to list/view rules. Use [the example rules](/ruxbase/integrations/example-app/) for a disposable test rather than copying a universal permissive policy.

## Filters are not authorization
A client filter only narrows accessible records. Use the SDK parameter helper for user-supplied filter values:
```js
const filter = pb.filter("title ~ {:q}", { q: searchTerm });
const result = await pb.collection("posts").getList(1, 20, { filter });
```
This is a variant of the tested filter call in [SDK integration](/ruxbase/integrations/javascript/). PostgreSQL comparison behavior and explicit sort are documented in [compatibility](/ruxbase/migration/compatibility/).

## Validate access
Test guest, owner, and a separate authenticated user with the same application configuration. Check that disallowed operations do not expose records and that hidden fields are absent. A 404 can represent denied view/update/delete; a list with no visible rows is not evidence of database emptiness.

Direct SQL is a separate boundary. Read [RLS policy compilation and limits](/ruxbase/authorization/row-level-security/); request-dependent rules need not be compilable for direct SQL. Do not assume the privileged application role is constrained by all generated policies.

## Evidence
[Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js) includes guest filtering and denied ordinary-user writes. [Source: docs/RLS.md](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/docs/RLS.md) defines direct database limitations.
