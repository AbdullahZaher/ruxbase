---
title: JavaScript and TypeScript SDK
description: Install the tested PocketBase JS SDK and connect, authenticate, query, upload and subscribe from Ruxbase clients.
sidebar:
  order: 3
---

**Development documentation.** Run examples against a disposable Ruxbase instance. Never embed a database connection string or a superuser credential in a client application.

## Install and connect

The repository tests pin the npm package pocketbase@0.28.1. Use the server origin, not the documentation site. A browser SPA may reuse a single client for the current application user; SSR must not share a user-authenticated client across requests.

```sh
npm install --save-exact pocketbase@0.28.1
```

```js
import PocketBase from "pocketbase";
const pb = new PocketBase("http://127.0.0.1:8090");
await pb.health.check();
```

Build [the example posts collection](/ruxbase/integrations/example-app/) and create a normal user before the next steps.

## Authenticate an application user

```js
await pb.collection("users").authWithPassword(email, password);
const userId = pb.authStore.record.id;
```

The inputs email and password come from your application UI or test fixture, not hardcoded production credentials. Handle errors without printing tokens. MFA may require an additional challenge; this example assumes the configured password-only test flow. Refresh with authRefresh(), and sign out locally with pb.authStore.clear(). Local token state alone is not server-side authorization.

## Create and list records

```js
const post = await pb.collection("posts").create({
    title: "First article",
    author: userId,
    published: false,
});
```

Use the SDK filter helper to bind filter values rather than concatenate user-controlled strings. The following function is also imported by the repository SDK compatibility tests against a running disposable server:

```js
export async function listPosts(client, collectionName, searchTerm, minimumViews) {
    return client.collection(collectionName).getList(1, 2, {
        filter: client.filter("title ~ {:q} && views >= {:min}", {
            q: searchTerm,
            min: minimumViews,
        }),
        sort: "views",
    });
}
```

Call listPosts(pb, "posts", "article", 0). The result has items, page, perPage and totals unless total counting is skipped. Use a stable explicit sort where order matters; for pagination with ties, add a deterministic secondary key. Fetch one with getOne(id), update with update(id, data), delete with delete(id). Collection rules still apply.

## Files and relation expansion

To upload a file, use FormData and a collection file field. The file variable below is selected by the current user.

```js
const data = new FormData();
data.append("cover", file);
const updated = await pb.collection("posts").update(post.id, data);
const fileUrl = pb.files.getURL(updated, updated.cover);
```

File URLs and protected-file access follow the field and view rules. Do not log token-bearing URLs. Use getOne(post.id, { expand: "author" }) to request the relation. Expansion can omit the author when its collection view rule denies access.

## Realtime and framework lifecycle

```js
await pb.collection("posts").subscribe("*", (event) => {
    // Update or invalidate local application data.
    console.log(event.action, event.record.id);
});
// On component cleanup or logout:
await pb.collection("posts").unsubscribe("*");
```

Subscriptions use SSE. Browser frameworks such as React, Vue and Svelte can invoke the same functions in their data layer; scope subscriptions to the screen or central data service that owns them. Avoid duplicate subscriptions and reconcile with a fresh list after reconnect. Do not assume durable delivery or exactly-once event processing. For non-browser runtimes use a compatible EventSource implementation when needed; the repository Node tests explicitly install eventsource 5.1.3.

## Evidence and limitations

The SDK suite exercises password authentication, refresh, invalid credentials, record CRUD, multipart upload and download, filtered pagination, field selection, relation expansion, rule denials and realtime. The shared listPosts function is covered by that suite. TypeScript uses the SDK's types; application collection field types remain application-defined. React/Vue/Svelte framework-specific rendering, OAuth/MFA workflows and device behavior are not certified by this suite. See [Ruxbase JS SDK test suite](https://github.com/AbdullahZaher/ruxbase/blob/d03ba38b8fa2063cd1479c9198c44de21a16de93/tests/sdk/js/sdk.test.js), [SDK upstream](https://github.com/pocketbase/js-sdk), [compatibility](/ruxbase/migration/compatibility/) and [REST](/ruxbase/integrations/rest/).
