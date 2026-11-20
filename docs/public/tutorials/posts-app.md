---
title: "Build a complete local posts application"
description: "Run a small browser frontend and Ruxbase backend with ordinary-user login and draft visibility."
---

**Development documentation.** Use disposable development data and ordinary record credentials for application clients. Verify the installed server version before using examples.

## What you will build
A browser page supports login, logout, draft creation and listing posts. It uses same-origin HTTP requests to the local backend and keeps its session token in memory only. A reload signs the user out. It is deliberately a small local example, not a feature-complete or production-certified app.

## Run
Configure the ordinary users and posts collection from [the example schema and rules](/ruxbase/integrations/example-app/). Build the normal binary at the repository root and run:
```sh
go build -o ruxbase ./examples/base
./ruxbase serve --dev --publicDir=./examples/docs-posts
```
Open http://127.0.0.1:8090/ and sign in as an ordinary test user. The UI is served by the server's static route; the administration dashboard remains at /_/. The source files are examples/docs-posts/index.html, app.mjs and client.mjs.

## Verify with two users
Create a draft as Alice. Alice can read it; Bob and a guest cannot see it until the configured list rule permits it. Use the dashboard only as authorized administrator for setup. Record content is rendered with textContent rather than injected HTML. The demo assumes password-only login, does not persist tokens, and does not implement a full production session, MFA, error handling or realtime workflow.

## Evidence
The exact client module is imported in the repository SDK compatibility suite to exercise ordinary password login, create and guest/second-user visibility. The browser UI is a small authored sample; the runtime HTTP suite does not certify a framework UI, full accessibility or production behavior. See [HTTP contract](/ruxbase/integrations/rest/) and [access rules](/ruxbase/concepts/rules/).
