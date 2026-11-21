# Wave 05 — Milestone 03: Reconnect, Cursor/Resync & Bounded Replay Semantics

## 1. Executive Summary & Objective

In real-world networks, client connections over Server-Sent Events (SSE) inevitably drop due to cellular handoffs, Wi-Fi blips, proxy timeouts, or server restarts. Without replay and cursor synchronization, clients silently miss database mutations, causing UI state divergence and data corruption.

Milestone **W05-03 / P1 / CORE** delivers robust reconnect, cursor resynchronization, bounded replay, and backpressure handling for Ruxbase real-time subscriptions.

Key guarantees:
1. **Standard `Last-Event-ID` Cursor Protocol:** Subscribers reconnecting to `/api/realtime` via the standard W3C `Last-Event-ID` header or `?lastEventId=` query parameter resume event streaming from their last acknowledged mutation.
2. **Bounded Replay Window ($\le 5\text{ minutes}$):** The PostgreSQL-backed transactional buffer `_realtime_events` maintains a rolling 5-minute replay retention window (`RealtimeEventsMaxAge = 5 * time.Minute`). Reconnecting clients receive any events committed after their cursor in strict chronological order.
3. **Explicit Stale Cursor Signaling (`PB_RESYNC_REQUIRED`):** If a client reconnects with an expired cursor (older than 5 minutes) or an unknown cursor, the server sends a structured `PB_RESYNC_REQUIRED` event instructing the client to perform a full REST state synchronization.
4. **Bounded Buffering & Backpressure Protection:** Subscription channels are bounded with a capacity of 256 messages. If a slow or stalled subscriber's buffer overflows, the server invokes backpressure protection: the connection is dropped gracefully (`Client.Discard()`) to prevent unbounded memory growth and goroutine exhaustion.

---

## 2. Protocol Specification

### 2.1 Event ID Generation
Every real-time event dispatched over SSE includes a unique, monotonically orderable event identifier:
```http
id: 1732224000123_a1b2c3d4e5
event: posts/*
data: {"action":"create","record":{"id":"...","title":"Hello"}}
```

The format `<timestamp_ms>_<random_entropy>` guarantees:
- Microsecond/millisecond chronological ordering.
- Collision resistance across multi-node clusters.
- Fast index lookups against `_realtime_events.created`.

### 2.2 Reconnect & Replay Flow

```
+--------------------+                         +----------------------+
|     SSE Client     |                         |     Ruxbase Node     |
+--------------------+                         +----------------------+
          |                                               |
          |  GET /api/realtime                            |
          |  Last-Event-ID: 1732224000123_a1b2c3d4e5      |
          |---------------------------------------------->|
          |                                               |
          |                                               |--+ Check cursor age
          |                                               |  | against 5m retention
          |                                               |<-+
          |                                               |
          | [Case 1: Cursor Valid & Within 5m Window]     |
          |-------------------------------------------    |
          |  event: PB_CONNECT                            |
          |  data: {"clientId":"..."}                     |
          |<- - - - - - - - - - - - - - - - - - - - - - - |
          |  Replayed Events (in order):                  |
          |  id: 1732224000124_...                        |
          |  event: posts/*                               |
          |<- - - - - - - - - - - - - - - - - - - - - - - |
          |  Live Events resume...                        |
          |                                               |
          | [Case 2: Cursor Expired / Stale]              |
          |-------------------------------------------    |
          |  event: PB_RESYNC_REQUIRED                    |
          |  data: {"code":"cursor_expired"}              |
          |<- - - - - - - - - - - - - - - - - - - - - - - |
          |  Client performs full GET /api/collections    |
```

### 2.3 Backpressure Protection
- Each connected client allocates a bounded channel `chan Message` with capacity 256.
- In `Client.Send(msg)`:
  - If buffer has space: message enqueued non-blocking.
  - If buffer is full (slow consumer): client is discarded (`c.Discard()`), terminating the connection and freeing resources.

---

## 3. Verification & Acceptance Criteria

| Check | Scenario | Expected Result | Disposition |
|---|---|---|---|
| **Cursor Replay** | Reconnect with valid cursor after 2 events | Client receives 2 missed events in order before live stream resumes | PASS |
| **Expired Cursor** | Reconnect with timestamp > 5m in past | Server responds with `PB_RESYNC_REQUIRED` message | PASS |
| **Backpressure** | Stalled client with full 256-message buffer | Server drops connection cleanly without leaking goroutines | PASS |
