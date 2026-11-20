---
title: "Records and CRUD"
description: "Read and write collection records using scoped operation contracts and pagination."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Record lifecycle
Record operations use the collection's schema and access rules. An authenticated client is not automatically allowed to read or modify every record.

| Operation | SDK method | HTTP method |
| --- | --- | --- |
| List | getList(page, perPage, options) | GET collection records |
| Read one | getOne(id) | GET record ID |
| Create | create(data) | POST collection records |
| Modify | update(id, data) | PATCH record ID |
| Remove | delete(id) | DELETE record ID |

Use the exact routes and response statuses in [API reference](/ruxbase/reference/api/). The record body is application-specific. A deletion returns an empty success body; do not parse it as JSON.

## Query, project and paginate
```js
const records = await pb.collection("posts").getList(1, 20, {
    sort: "-views,id",
    fields: "id,title,views",
});
```
Initialize pb as shown in [JavaScript SDK](/ruxbase/integrations/javascript/). This is an illustrative variant of tested list, sort and field selection calls. Request only the fields you need and use a secondary sort key for ties; physical database order is not stable. With skipTotal enabled, totals can be -1. Do not interpret a filtered empty list as proof no record exists.

## Evidence
[Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js) exercises CRUD, filters, projection, pagination and rule denials. [REST client](/ruxbase/integrations/rest/) contains the tested direct HTTP list helper. Review [access rules](/ruxbase/concepts/rules/) before building write forms.
