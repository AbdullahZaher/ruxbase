---
title: "Relations and expansion"
description: "Use one-to-one or multiple record references and understand visibility of expanded records."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Relationship model
A relation field points to one target collection. A single relation stores a record ID; a multiple relation stores a list of record IDs. Choose cardinality using the field configuration and verify requiredness and deletion behavior. Do not assume that a relation implies the caller can read its target.

## Retrieve related records
For the posts schema, author points to users:
```js
const post = await pb.collection("posts").getOne(postId, {
    expand: "author",
});
const author = post.expand?.author;
```
A related record may be absent from expand even if its ID is present. Expanded records follow their own collection view rules. Handle this case in the UI rather than bypassing authorization by fetching as an administrator.

## Evidence and limits
The existing SDK test checks that another user cannot expand Alice's auth record while Alice can. [Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js); [Source: core/field_relation.go](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/core/field_relation.go). Back-relations, deep expansion and every cascade configuration need focused tests before reliance. See [rules](/ruxbase/concepts/rules/) and [field values](/ruxbase/concepts/fields/).
