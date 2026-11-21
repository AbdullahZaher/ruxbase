# Wave 04: Unified Data API & Developer Experience — Starters, Docker Compose & AI Scaffolding

```
Document ID:     RUXBASE-WAVE04-STARTERS-AND-COMPOSE
Version:         1.0.0
Milestone:       W04-06 (Working React/Next.js starter projects, Docker Compose examples and AI generation smoke tests)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Milestone W04-06 delivers production-grade frontend starter applications, turnkey container compositions, and deterministic AI generation contracts:
1. **React Starter (`starters/react`)**: Client-side React 19 SPA with Vite, Tailwind CSS v4, and official PocketBase JavaScript SDK client.
2. **Next.js Starter (`starters/nextjs`)**: Modern Next.js 15 App Router application with secure SSR session handling, request origin verification, and isolated authentication proxies.
3. **Turnkey Docker Compose Templates**: Reproducible container environments (`docker-compose.yml`, `docker-compose.dev.yml`) orchestrating Ruxbase with PostgreSQL 17/18 and `pgvector`.
4. **Deterministic AI Project Generation**: `scripts/create-ruxbase-app.mjs` executes safely without network requests, shell execution, or package mutations; validated against prompt injection in `starters/ai-tasks.json`.

---

## 2. Starter Architecture & Security Guarantees

| Starter | Framework | Toolchain | Security Boundary |
|---|---|---|---|
| `react` | React 19 | Vite, Tailwind CSS v4, `pocketbase` SDK | Public client with ordinary user credentials; zero admin tokens in frontend code. |
| `nextjs` | Next.js 15 | App Router, Server Components, SSR Cookie Store | Server-side API proxy with origin validation; secrets kept exclusively on server. |

### Safe Scaffolding Engine
- Command: `node scripts/create-ruxbase-app.mjs <framework> <name> [--output dir]`
- Refuses path traversal and symlinks.
- Atomic directory creation via `mkdir(target)`.
- Validates locked npm dependency graph (`lockfileVersion: 3`).
- Writes default `.env.example` and `RUXBASE_AI_CONTEXT.md`.

---

## 3. Automated Verification

Executable verification runs across multiple test layers:
- Static lockfile integrity: `node scripts/check-starter-locks.mjs`
- Generator and doctor unit tests: `node --test scripts/test-create-ruxbase-app.mjs scripts/test-ruxbase-doctor.mjs`
- AI evaluation and safety harness: `node --test scripts/test-ai-project-eval.mjs`
- End-to-end Go test: `tests/e2e/starters_compose_smoke_test.go`
