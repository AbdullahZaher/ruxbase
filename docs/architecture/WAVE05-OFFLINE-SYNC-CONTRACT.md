# Wave 05 — Offline Sync Contract & Conflict Policy

## 1. Executive Summary

This specification establishes the architectural contract for **Offline Local Cache, Client Synchronization, Deterministic Conflict Resolution (LWW), and Resumable Device Sync** in Ruxbase (Milestone W05-06).

While Ruxbase provides live SSE and ephemeral WebSocket broadcast/presence channels, mobile and field-edge applications require robust operation during network degradation and offline operation. This document formalizes the offline cache layout, the pull delta sync protocol, the push batch mutation protocol, the conflict resolution policy, and resumable device sync boundaries.

---

## 2. Client-Side Offline Local Cache Model

Clients (web via IndexedDB, mobile via SQLite/AsyncStorage) maintain a structured local cache with three core tables:

1. **Entity Cache (`entities_<collection>`)**:
   - `id`: Canonical 15-character string record identifier.
   - `data`: JSON representation of record fields.
   - `created`: Record creation timestamp (UTC RFC3339).
   - `updated`: Record last-modified timestamp (UTC RFC3339).
   - `_sync_status`: `"synced"` | `"pending_create"` | `"pending_update"` | `"pending_delete"`.

2. **Pending Mutations Queue (`pending_mutations`)**:
   - `mutation_id`: Locally generated unique sequence ID.
   - `collection`: Target collection name.
   - `action`: `"create"` | `"update"` | `"delete"`.
   - `record_id`: Record ID.
   - `data`: Mutation payload.
   - `client_updated_at`: Timestamp when the user performed the local mutation.
   - `attempts`: Retry counter.

3. **Sync Metadata (`sync_metadata`)**:
   - `collection`: Collection name.
   - `last_synced_at`: Server cursor (UTC RFC3339) representing the last successful delta pull.

---

## 3. Synchronization Protocol

### 3.1 Delta Pull Sync (`GET /api/realtime/sync`)

Enables a client to synchronize all server modifications made since its last recorded sync timestamp.

- **Request**:
  ```http
  GET /api/realtime/sync?collection=notes&since=2026-11-21T20:00:00Z HTTP/1.1
  Authorization: Bearer <user_token>
  X-Tenant-Id: <optional_tenant_id>
  ```
- **Response** (HTTP 200 OK):
  ```json
  {
    "collection": "notes",
    "syncedAt": "2026-11-21T21:55:00Z",
    "records": [
      {
        "id": "rec001000000000",
        "title": "Updated note title",
        "created": "2026-11-21T19:00:00.000Z",
        "updated": "2026-11-21T21:30:00.000Z"
      }
    ],
    "deleted": [
      "rec002000000000"
    ]
  }
  ```

#### Implementation Semantics:
1. **Authorization**: Validates collection existence and verifies that the caller satisfies the collection's `ViewRule` and `ListRule`. If tenant isolation is active, records are scoped to `X-Tenant-Id` or auth tenant.
2. **Updated Records**: Queries records from the target collection table where `updated >= since`.
3. **Tombstone Deleted Records**: Queries `_realtime_events` for `op IN ('delete', 'd') AND collection_id = collection.Id AND created >= since`.
4. **New Cursor**: Returns server timestamp `syncedAt` (UTC RFC3339) which the client commits as its new `last_synced_at`.

---

### 3.2 Batch Push Sync (`POST /api/realtime/sync`)

Enables a client to transmit offline mutations accumulated during network disconnection in a single atomic transaction.

- **Request**:
  ```http
  POST /api/realtime/sync HTTP/1.1
  Authorization: Bearer <user_token>
  Content-Type: application/json

  {
    "collection": "notes",
    "mutations": [
      {
        "action": "create",
        "id": "newnote00000001",
        "data": { "title": "Field Note A" },
        "clientUpdatedAt": "2026-11-21T21:00:00Z"
      },
      {
        "action": "update",
        "id": "rec001000000000",
        "data": { "title": "Offline Revision" },
        "clientUpdatedAt": "2026-11-21T21:10:00Z"
      },
      {
        "action": "delete",
        "id": "rec003000000000",
        "clientUpdatedAt": "2026-11-21T21:15:00Z"
      }
    ]
  }
  ```
- **Response** (HTTP 200 OK):
  ```json
  {
    "collection": "notes",
    "syncedAt": "2026-11-21T21:56:00Z",
    "applied": [
      { "id": "newnote00000001", "action": "create", "status": "applied" },
      { "id": "rec001000000000", "action": "update", "status": "applied" },
      { "id": "rec003000000000", "action": "delete", "status": "applied" }
    ],
    "conflicts": []
  }
  ```

---

## 4. Conflict Resolution Policy (Deterministic LWW)

When concurrent writes occur between offline devices and the central server, Ruxbase applies **Deterministic Last-Write-Wins (LWW)**:

1. **For `update` mutations**:
   - The server inspects the existing record's `Updated` timestamp.
   - If `clientUpdatedAt < record.Updated` (the server record was modified by another client *after* the offline user made their local change):
     - The server version wins.
     - The mutation is flagged with `status: "conflict_server_won"`.
     - The current server record is returned in the response so the client can update its local cache immediately without a full re-fetch.
   - If `clientUpdatedAt >= record.Updated`:
     - The client version wins.
     - The record is updated and saved.
     - The mutation is marked `status: "applied"`.

2. **For `delete` mutations**:
   - If the record was already deleted, the operation succeeds idempotently (`status: "applied"`).
   - If the record was updated after `clientUpdatedAt`, LWW applies: if server update is newer, delete is rejected with `status: "conflict_server_won"`.

3. **For `create` mutations**:
   - If the record ID already exists:
     - If client update is newer, it updates the record.
     - Otherwise, flagged as `status: "conflict_already_exists"`.

---

## 5. Resumable Device Sync Boundaries

- **Delta Retention**: `_realtime_events` records deleted snapshots. If a client has been offline longer than the event retention window (or if `since` is empty/invalid), the server signals `resyncRequired: true`, instructing the client to perform a full baseline refresh.
- **Tenant & Role Boundaries**: All mutations are executed under the caller's authorization context. An offline user cannot push mutations to records outside their tenant or permissions.
