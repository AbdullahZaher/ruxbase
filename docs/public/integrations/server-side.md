---
title: Server-side and SSR integration
description: Use request-scoped SDK clients in Node and server-rendered apps without sharing user authentication state.
sidebar:
  order: 4
---

**Development documentation.** Run examples against a disposable Ruxbase instance. Never embed a database connection string or a superuser credential in a client application.

## Request-scoped authentication

Node.js, Next.js, Nuxt and SvelteKit server handlers should allocate a new SDK client for each incoming request. Never share a mutable user auth store between concurrent requests. A browser singleton is not a server singleton for multiple users.

Use a trusted runtime configuration for the Ruxbase origin, not a URL supplied by a requester. Treat incoming cookies as untrusted. For protected server routes, validate the session with the server before allowing access; isValid checks token expiry but is not proof of a verified server session.

## Framework-neutral session adapter

This illustrative adapter uses the upstream SDK's cookie functions and a request-scoped client. It has not been executed in a framework-specific integration test.

```js
import PocketBase from "pocketbase";

export async function loadSession(baseUrl, cookieHeader) {
    const pb = new PocketBase(baseUrl);
    pb.authStore.loadFromCookie(cookieHeader ?? "");
    let user = null;
    if (pb.authStore.isValid) {
        try {
            const result = await pb.collection("users").authRefresh();
            user = result.record;
        } catch {
            pb.authStore.clear();
        }
    } else {
        pb.authStore.clear();
    }
    const setCookie = pb.authStore.exportToCookie({
        httpOnly: true,
        secure: true,
        sameSite: "lax",
        path: "/",
    });
    return { pb, user, setCookie };
}
```

Use HTTPS for the secure cookie. Only an authenticated result authorizes a protected route; a failed refresh returns no user and must not be treated as success. Do not serialize the SDK instance or auth token to the browser. Send the updated Set-Cookie header on the actual server response, including on signed-out or rejected sessions. Add a separate login/logout flow, CSRF protections for cookie-authenticated state-changing routes, private/no-store caching where needed and your application's authorization checks. Handle an MFA challenge explicitly instead of treating it as completed login.

## Map the pattern to a framework

| Framework | Where to create/request the client |
| --- | --- |
| Next.js | Server route handler or server action, using request cookies and response cookie APIs |
| Nuxt | Per-request Nitro server handler or middleware |
| SvelteKit | Per-request server hook, client held in event.locals |
| Node.js HTTP server | Request handler, never module-global user state |

These are integration locations, not tested adapters. Server-only variables must not be bundled into client code. Protect cookies and never expose superuser credentials. Revalidate on protected operations and avoid sharing cached responses across users.

## Evidence and limitations

Ruxbase's JS SDK suite tests password auth and refresh, but not SSR cookie round-trips, framework adapters, CSRF or concurrent user isolation in those frameworks. The adapter pattern is based on [upstream SDK cookie guidance](https://github.com/pocketbase/js-sdk) and must be tested in the target application before deployment. See [JavaScript SDK](/ruxbase/integrations/javascript/) and [example API rules](/ruxbase/integrations/example-app/).
