---
title: Developer tooling and reproducible starters
description: Use the safe Ruxbase CLI, locked dependencies, local diagnostics and bounded real-model AI evaluation.
---

> Development workflow only. Neither the generator nor the starter has been approved as a production application.

## Create an application reproducibly

From a Ruxbase source checkout (Node.js 25.2.1 in CI), choose one of the two supported templates:

```sh
node scripts/create-ruxbase-app.mjs --list
node scripts/create-ruxbase-app.mjs --framework react --name my-react-app --output ./projects
node scripts/create-ruxbase-app.mjs --framework nextjs --name my-next-app --output ./projects
```

Run `node scripts/create-ruxbase-app.mjs --help` for the full CLI contract. The positional syntax from Batch 01 remains supported: `node scripts/create-ruxbase-app.mjs react my-react-app`.

For automation and AI agents, first inspect the plan without writing files, then request one structured result:

```sh
node scripts/create-ruxbase-app.mjs --framework react --name my-app --dry-run --json
node scripts/create-ruxbase-app.mjs --framework react --name my-app --json
cd my-app
npm ci
npm run build
npm run dev
```

The CLI rejects duplicate options, unknown templates, path traversal, invalid npm names, existing directories and symlinks in the bundled template. It uses exclusive destination directory creation to prevent accidental overwrite and cleans up a partially created project after an internal error. **It never installs packages, runs arbitrary shell commands or generates credentials.**

Both templates commit `package-lock.json` with npm lockfile v3, pinned direct dependencies and resolved package integrity. Generation rewrites both the package name and the lockfile root names while retaining the pinned transitive dependency graph. CI performs an actual `npm ci` build, vulnerability audit, browser test and live Ruxbase/PostgreSQL test; `npm install` is not part of the certified install path.

## Backend and common setup errors

First start Ruxbase using [Docker Compose Quickstart](/ruxbase/getting-started/quickstart/), and configure an ordinary `users` account and `posts` collection with the [documented rules](/ruxbase/integrations/example-app/). No admin credential or PostgreSQL URL belongs in a frontend application.

| Symptom | Diagnosis |
| --- | --- |
| `FRAMEWORK` / `NAME` CLI error | Use `react` or `nextjs`, lowercase npm-safe name 2–50 chars |
| `EXISTS` CLI error | Choose a new directory; the generator deliberately never overwrites |
| `npm ci` reports manifest mismatch | Restore the committed lockfile and matching `package.json`; never bypass with a relaxed install |
| Backend not connected / 502 | Check `curl --fail http://127.0.0.1:8090/api/health?ready` and Compose health |
| Posts collection missing / 404 | Create the exact schema and list/view rules |
| Login rejected / 400 | Use an ordinary application user with configured password authentication |
| Next.js server mutation returns 403 | Set trusted `RUXBASE_APP_ORIGIN` and send same-origin JSON requests with the expected Origin header |

Use a disposable database while developing. The root Docker stack is not a production least-privilege database template.

## Protected Next.js server sessions

Batch 02 adds a **separate** authenticated server-route example to the Next.js starter, in addition to its original browser-only app. It validates incoming user cookies against Ruxbase on each request and checks explicit CSRF origin constraints. See [Protected Next.js sessions](/ruxbase/tutorials/nextjs-protected-sessions/) for the tested route contract and remaining security limitations.

## AI agent evaluation: deterministic versus actual model

The required starter CI deterministically creates and runs projects, including real browser login, two-user owner-rule denial and frontend Docker containers. It **does not mean an LLM independently generated source code**.

A separate manually dispatched [`docs-ai-model-eval` workflow](https://github.com/AbdullahZaher/ruxbase/actions/workflows/docs-ai-model-eval.yml) is available to evaluate actual model responses. An owner must intentionally configure the `RUXBASE_AI_EVAL_API_KEY` repository secret and the `RUXBASE_AI_EVAL_MODEL` repository variable before invoking it. This consumes the selected external model API and may incur charges; no automatic PR/push trigger calls it.

The workflow asks a real model to select the **approved generator tool** for four bounded React/Next.js tasks (including two hostile-instruction tasks). Only a strictly validated `{tool, framework, name}` plan is accepted; the model cannot pass source code, shell commands, network endpoints or credentials to the execution job. A second job, which does not receive the model API key, compiles the model-selected projects with `npm ci`, runs them against disposable Ruxbase/PostgreSQL and checks Chromium interactions and owner-only records.

This is a **real-model tool-selection and executable-scaffold evaluation**, not an unrestricted AI source-code quality benchmark, multi-model comparison, proof of prompt-injection resistance in general, or a claim that the optional workflow has run. No external model PASS is reported without an actual successful invocation and retained evidence.

See [Create an application](/ruxbase/getting-started/create-app/) and [React](/ruxbase/tutorials/starter-react/) or [Next.js](/ruxbase/tutorials/starter-nextjs/) tutorials.
