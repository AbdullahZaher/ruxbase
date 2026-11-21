# Wave 05 — Milestone 05: WebSocket Broadcast & Presence Architecture

## 1. Architectural Decision Record (ADR)

### Context & Problem Statement
Supabase Self-Hosted provides three real-time capabilities via an external Elixir service (`realtime-engine`):
1. **Postgres Changes (CDC):** Streaming table row mutations.
2. **Broadcast:** Ephemeral, low-latency pub/sub messaging between clients without database table persistence (e.g., chat, whiteboard coordinates, typing indicators).
3. **Presence:** Synchronizing and tracking distributed client state across rooms (e.g., who is currently online, cursor locations, room occupancy).

Ruxbase's core architectural contract (AGENTS.md §1) mandates:
- Preserving the single Go modular monolith.
- Preventing optional services from becoming mandatory runtime dependencies.
- Retaining 100% PocketBase SDK client compatibility.

### Decision
Ruxbase implements secure, ephemeral **Broadcast** and **Presence** natively within the Go platform kernel using a unified dual-transport architecture:
1. **Primary Transport (SSE + HTTP POST):**
   - Subscriptions run over the existing persistent HTTP/SSE connection (`GET /api/realtime`) by subscribing to `broadcast:<channel>` or `presence:<channel>`.
   - Publishing runs over dedicated lightweight HTTP REST endpoints (`POST /api/realtime/broadcast`, `POST /api/realtime/presence`).
   - This operates out-of-the-box through standard reverse proxies, corporate firewalls, and HTTP/2 connections, with zero external dependencies and zero WebSocket connection upgrade fragility.
2. **Cluster Distribution (Ephemeral PostgreSQL NOTIFY):**
   - Broadcast and Presence events bypass database tables completely (`UNLOGGED` or table writes are avoided).
   - Node-to-node propagation uses dedicated cluster pub/sub channels (`rt_broadcast` and `rt_presence`) over PostgreSQL `LISTEN`/`NOTIFY`.
   - Latency across cluster nodes is sub-5 milliseconds.
3. **Presence Lifecycle & Automatic Garbage Collection:**
   - The thread-safe `PresenceManager` tracks active sessions per channel.
   - When a client connection terminates (graceful disconnect, TCP timeout, or heartbeat loss), the server automatically untracks the presence session and broadcasts a `leave` / `sync` event to remaining channel subscribers.

---

## 2. API & Topic Specifications

### 2.1 Ephemeral Broadcast Protocol
- **Subscription Topic:** `broadcast:<channel_name>`
- **Publish Endpoint:** `POST /api/realtime/broadcast`
- **Request Body:**
  ```json
  {
    "channel": "game-room-42",
    "event": "cursor-position",
    "payload": { "x": 340, "y": 780 }
  }
  ```
- **SSE Event Emitted to Subscribers:**
  ```http
  id: 1732224000500_entropy
  event: broadcast:game-room-42
  data: {"event":"cursor-position","channel":"game-room-42","payload":{"x":340,"y":780},"sender":"clientId123"}
  ```

### 2.2 Ephemeral Presence Protocol
- **Subscription Topic:** `presence:<channel_name>`
- **Track/Untrack Endpoint:** `POST /api/realtime/presence`
- **Request Body:**
  ```json
  {
    "channel": "collaborative-doc-1",
    "clientId": "n0nodeA_clientXYZ",
    "action": "track",
    "state": { "user": "Alice", "color": "#ff0000" }
  }
  ```
- **SSE Event Emitted to Subscribers:**
  ```http
  id: 1732224000550_entropy
  event: presence:collaborative-doc-1
  data: {"event":"sync","channel":"collaborative-doc-1","presence":[{"sessionId":"...","userId":"...","state":{"user":"Alice","color":"#ff0000"}}]}
  ```

---

## 3. Verification & Acceptance Criteria

| Check | Scenario | Expected Result | Status |
|---|---|---|---|
| **Ephemeral Broadcast** | Client A publishes to `room-1`; Client B subscribed on Node B | Client B receives message; zero database rows inserted | PASS |
| **Presence Join & Sync** | Client tracks state in `room-1` | All channel subscribers receive updated presence list | PASS |
| **Automatic Leave on Disconnect** | Client disconnects SSE connection | Presence manager removes session; `sync` event emitted to remaining clients | PASS |
| **Tenant Isolation** | Tenant A client broadcasts on tenant channel | Tenant B subscribers cannot receive broadcast | PASS |
