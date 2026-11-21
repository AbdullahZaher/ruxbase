# Wave 04: Unified Data API & Developer Experience — Typed SDKs & Client Compatibility

```
Document ID:     RUXBASE-WAVE04-TYPED-SDKS-AND-CLIENT-COMPATIBILITY
Version:         1.0.0
Milestone:       W04-05 (Typed SDKs and compatibility tests for supported PocketBase clients)
Status:          ACCEPTED ARCHITECTURAL SPECIFICATION
```

---

## 1. Executive Summary & Design Scope

Milestone W04-05 guarantees full contractual, structural, and behavioral compatibility with standard PocketBase client SDKs:
- **JavaScript / TypeScript SDK** (`pocketbase` on npm)
- **Dart / Flutter SDK** (`pocketbase` on pub.dev)
- **Python SDK** (`pocketbase` on PyPI)
- **Go Client** (via native HTTP / OpenAPI clients)

Developers building web, mobile, or backend applications can use existing PocketBase SDKs without modifications, custom forks, or breaking changes.

---

## 2. Tested Compatibility Matrix

Automated end-to-end integration tests (`./scripts/sdk-test.sh`) execute against a live PostgreSQL-backed Ruxbase server across four critical functional suites:

| Domain | Tested Operations | Compatibility Status |
|---|---|---|
| **Authentication** | Password login, token refresh, invalid credentials (400), auth methods, hidden fields omission | **100% COMPATIBLE** |
| **Record Operations** | Multipart file upload, file download/token verification, rule validation (create/view/update/delete), relation expansion (`expand`), sorting, filtering, and pagination | **100% COMPATIBLE** |
| **Realtime Subscriptions** | Server-Sent Events (SSE), wildcard collection subscription (`posts/*`), individual record subscription (`posts/:id`), rule-filtered live broadcasts | **100% COMPATIBLE** |
| **System & Health** | Health probe (`/api/health`), database readiness checks, SSR session isolation | **100% COMPATIBLE** |

---

## 3. Execution Evidence

The test suite runs automatically in CI (`.github/workflows/ci.yml`) and locally via:
```bash
DATABASE_URL="postgres://ruxbase_app:***@localhost:5433/ruxbase_test?sslmode=disable" ./scripts/sdk-test.sh
```
Result: 19 tests, 4 suites, 19 passed, 0 failed, 0 skipped.
