---
title: Protected Next.js server sessions
description: Request-scoped Ruxbase SDK authentication in Next.js App Router with cookies, CSRF checks, owner-only records and negative tests.
---

> A bounded integration example, not an independently security-certified production authentication system.

The React and Next.js default starter UI uses a memory-only browser SDK. Batch 02 introduces a **separate** server-side cookie-session example in the Next.js starter. Do not confuse it with automatically securing all client-side pages.

## Server routes

| Route | Method | Behavior |
| --- | --- | --- |
| `/api/session/login` | POST | Verify an ordinary user with Ruxbase; set an HttpOnly same-site session cookie |
| `/api/session/me` | GET | Create a fresh SDK client and validate the cookie with `users.authRefresh()` |
| `/api/session/posts` | GET | Return records allowed by the current user's Ruxbase collection rules |
| `/api/session/posts` | POST | Create an unpublished draft, deriving `author` exclusively from the validated user |
| `/api/session/logout` | POST | Clear the browser cookie |
| `/protected` | GET | Server-render an authenticated user's posts; redirect guests |

The implementation resides in `starters/nextjs/src/server/session.mjs` and the corresponding `app/api/session/**/route.js` files. Each request creates a new PocketBase client and an independent `BaseAuthStore`. An incoming token is not trusted merely because its expiration field is valid: protected handlers call `authRefresh()` against the running backend before returning user data.

All JSON API responses are private and `no-store`; the authenticated page is dynamic. The cookie uses `HttpOnly`, `SameSite=Lax`, `Path=/`, and `Secure` in production. It is a bearer token carried inside an HttpOnly cookie: do not log, expose or cache it.

## Configuration and CSRF boundary

Specify a **trusted server-side origin**, never one supplied by the requester:

```sh
RUXBASE_INTERNAL_URL=http://127.0.0.1:8090 \
RUXBASE_APP_ORIGIN=http://127.0.0.1:3000 \
npm run start
```

The root Compose overlay sets the internal backend to `http://ruxbase:8090` and a loopback application origin. Public production deployment requires HTTPS and explicit `RUXBASE_APP_ORIGIN`, with additional infrastructure and threat-model review.

Every mutation (login, logout and draft creation) must use a JSON request with an `Origin` matching the explicitly configured application origin. Missing/mismatching origins and cross-site Fetch Metadata are rejected; invalid JSON/body lengths are bounded. Cookie `SameSite=Lax` supplements but does not replace these checks. Keep trusted reverse-proxy Host/Origin configuration consistent.

Example same-origin browser request:

```js
const response = await fetch("/api/session/login", {
  method: "POST",
  credentials: "same-origin",
  headers: { "Content-Type": "application/json" },
  body: JSON.stringify({ email, password }),
});
if (!response.ok) throw new Error("Sign-in rejected");
```

Use ordinary user credentials from the current UI, not superuser credentials. The server response returns only the user's id/email, not the bearer token.

## Negative integration certification

The disposable GitHub Actions `docs-starters` job builds Next.js with its real toolchain, runs `next start` against Ruxbase/PostgreSQL, then checks:

- Two ordinary users and concurrent sessions do not exchange user identity or private records.
- Missing, forged and expired-shaped cookie values reject authenticated API and server-rendered views.
- Missing or foreign `Origin`, cross-site Fetch Metadata and wrong media types reject mutations.
- A user cannot impersonate another `author` or force a draft public by supplying extra JSON fields.
- Guest/other-user reads never return another user's private draft.
- Cookie attributes, private/no-store API output, protected SSR HTML and browser logout behavior remain intact.

These checks do not prove multi-node token revocation, OAuth/MFA, access-token theft recovery, advanced rate limiting, proxy configuration or external attack resistance. **Logout clears the cookie only; it does not revoke an already copied backend bearer token.** A further production security assessment is required before adapting this pattern for real customer accounts.

Read [server-side authentication](/ruxbase/integrations/server-side/) and [Next.js integration](/ruxbase/integrations/nextjs/) for the broader SSR boundary.

## Reverse-proxy Host boundary

Mutating endpoints validate both a fixed server-configured `RUXBASE_APP_ORIGIN` and the incoming HTTP `Host` header, plus JSON content type and `Sec-Fetch-Site`. Next.js may canonicalize `request.url` to its internal listener hostname; do not make the CSRF decision from that internal URL. Terminating proxies must preserve the **external public Host** and only trusted TLS routing may reach the application. Requests using a mismatched Host, missing Origin, or cross-site Fetch Metadata are rejected; arbitrary `X-Forwarded-Host` headers are not treated as authority. This remains an example requiring an infrastructure-specific security review before production use.
