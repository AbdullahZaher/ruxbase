---
title: "Next.js, Nuxt and SvelteKit server adapters"
description: "Apply request-scoped verified user sessions in server rendering and route handlers."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## Server-only boundary
For a runnable client-only App Router template with a trusted same-origin /api rewrite, see [Next.js starter tutorial](/ruxbase/tutorials/starter-nextjs/) and [Create an application](/ruxbase/getting-started/create-app/). The initial visual app remains client-only. The Next.js starter now also includes [a separately tested server-only cookie-session implementation](/ruxbase/tutorials/nextjs-protected-sessions/).

Use the [request-scoped session adapter](/ruxbase/integrations/server-side/) in a server-only module. The server must never share user auth state across requests. Read the request cookie, validate with the backend, return an updated cookie, and deny protected operations without a validated user.

| Environment | Adaptation point |
| --- | --- |
| Next.js App Router | Route handler or server action; read cookies and set response cookie using the supported framework API |
| Nuxt/Nitro | Server handler; event-scoped cookie APIs |
| SvelteKit | Server hook and event.locals; use the supported cookie API for response |
| Plain Node HTTP | Per-request handler using request/response headers |

Use the framework's current documentation for exact cookie and request APIs. Do not copy an outdated server component API into a route handler. Keep SDK client state server-only, use private/no-store responses for user-specific content and obtain separate application CSRF protections for cookie-authenticated mutations.

## Test before deployment
Simultaneously send requests with two distinct user sessions, a missing or invalid cookie and a rejected refresh. Assert returned records match the corresponding session, no auth state leaks between requests, and the cookie is cleared on rejected sessions. The shared SDK cookie adapter is exercised by the SDK suite. The Next.js App Router demo is independently exercised in docs-starters CI: protected route handlers, request-scoped authenticated HTML, CSRF rejection, cookie attributes, missing/invalid user sessions and concurrent Alice/Bob isolation against disposable Ruxbase/PostgreSQL. Nuxt and SvelteKit adapters, production proxy deployments, token revocation, OAuth/MFA and other auth threats remain untested.

## Evidence
[Source: tests/sdk/js/sdk.test.js](https://github.com/AbdullahZaher/ruxbase/blob/54be0f41431f45eb039bf877a96637f48ba1da43/tests/sdk/js/sdk.test.js) covers auth and refresh. The adapter uses the upstream SDK cookie methods. See [canonical adapter and security constraints](/ruxbase/integrations/server-side/).
