# WebSocket room snapshot and member broadcasts — Design

## First-principles analysis

### Challenge assumptions

- Assumption: the hub must implement every message in `docs/api/websocket.md`. Unverified and too broad for Issue #15; the issue explicitly excludes mute, speaking, reconnect state, chat, and cross-instance broadcasting.
- Assumption: LiveKit participant presence can drive member lists. Wrong for echo's architecture; product membership is owned by the business service and persisted room/member state.
- Assumption: WebSocket authentication can use `Authorization` headers. Wrong for browser/WebView2 clients; the contract requires a query `token`.
- Assumption: WebSocket state must be durable. Wrong for MVP; durable facts are room/member lifecycle rows, while connections, sequence counters, and heartbeat are in-memory single-instance state.
- Assumption: join/leave broadcast requires rewriting existing room service. Partially wrong; create/join/leave already exist, but broadcast requires a small event sink after successful mutations.

### Bedrock truths

- A WebSocket connection is an in-memory transport; once the process exits, connection state is gone.
- Authorization must be decided before trusting client payloads; a client can send any JSON after connection.
- The existing room session token cryptographically binds room ID, member ID, version, and expiry.
- The persisted database is the current product truth for rooms and members.
- A room snapshot is a projection, not a new source of truth; it must be rebuilt from persisted room/member state plus receiver identity.
- Multiple clients in one process need a single fan-out owner to avoid duplicated event sequencing and inconsistent broadcasts.
- Issue #15 acceptance is satisfied by valid credential gate, snapshot, join/leave broadcasts, heartbeat, and tests; reconnect/mute/speaking commands require additional state transitions not demanded here.

### Rebuild from truths

1. Accept a socket only after token verification and persisted room/member authorization pass.
2. Keep a single in-memory hub per API process with per-room connection sets and monotonically increasing room sequence counters.
3. Build snapshots from repository reads each time a connection is accepted or resync is requested; personalize only `is_self`/`self_member_id` at the edge.
4. Reuse existing HTTP join/leave service paths as the only source of product membership mutations, then notify the hub after success.
5. Implement heartbeat as transport liveness only; on timeout remove/close the connection without writing reconnecting/disconnected product states in this issue.
6. Keep unsupported contract messages recoverable via `room.error` where safe, because Issue #15 must not accidentally implement broader product state.

### Contrast with convention

A conventional all-in WebSocket room server might implement presence, reconnect, mute, speaking, resync, and message queues in one pass. That would exceed Issue #15, duplicate product authority, and increase the chance of breaking existing HTTP room lifecycle. The fundamental difference here is to implement only the smallest mechanism that satisfies the verified product truth: live member-list snapshot plus join/leave fan-out for already-authorized temporary-room members.

### Conclusion

Issue #15 should add a single-instance backend WebSocket hub integrated with existing room/session/store services, not a new distributed state system or LiveKit-derived presence layer.

## Architecture and boundaries

### New package

- `services/api/internal/ws`
  - Owns WebSocket envelope types, room snapshot projection, in-memory hub, per-room sequencing, connection lifecycle, heartbeat, and test helpers.
  - Depends on `domain`, `room` authorization/leave interfaces as abstractions, `session`, and a repository read interface.
  - Uses `github.com/coder/websocket` and `github.com/coder/websocket/wsjson` unless implementation discovers a blocking incompatibility.

### HTTP integration

- Extend `services/api/internal/http/router.go` with an option to inject a room WebSocket handler/hub.
- Register `GET /v1/rooms/:room_id/ws` only when configured.
- Existing `POST /v1/rooms/join` and `POST /v1/rooms/:room_id/leave` continue to own mutations. After successful mutation, handlers notify the hub of join/leave events.
- `cmd/api/main.go` wires one hub instance with the same room service, repository, credential config, and clock/config values used by HTTP.

### Persistence integration

- Add repository read helpers if needed:
  - load active members by room ID ordered by `joined_at` then stable ID;
  - optionally load room+active members in one method for snapshot consistency.
- Do not persist WebSocket connections, sequence counters, heartbeat pings, or token plaintext.
- Existing `LeaveRoomMember` and `JoinRoomWithMember` remain the mutation authority for lifecycle and capacity.

### Config

- Use contract default durations in code/config:
  - `heartbeat_interval_ms = 15000`
  - `heartbeat_timeout_ms = 30000`
  - `reconnect_window_ms = 30000`
- If new config fields are added, keep defaults explicit and add `FromEnv()` overlays only for documented `ECHO_*` keys. Do not invent deployment-required env variables unless needed.

## Data flow

### Connect

```text
Client GET /v1/rooms/{room_id}/ws?token=...
  -> HTTP router/hub extracts path room ID + query token
  -> session.Verify(secret, token, now)
  -> require claims.RoomID == path room ID
  -> room.AuthorizeMemberContext(room_id, member_id)
  -> WebSocket upgrade
  -> hub registers connection under room/member
  -> hub builds room snapshot from persisted room + active members
  -> hub sends room.snapshot to the connection
  -> heartbeat loop starts
```

### Join broadcast

```text
HTTP POST /v1/rooms/join
  -> existing room service validates invite/capacity and persists member
  -> handler returns HTTP response with credentials as before
  -> handler notifies hub: member joined(room, member)
  -> hub sends member.joined to existing room connections
```

The new member receives its own current state through its WebSocket `room.snapshot` when it connects. Existing clients receive `is_self: false` for the new member.

### Leave broadcast

```text
HTTP POST /v1/rooms/{room_id}/leave
  -> existing room service marks member disconnected and updates empty-room retention
  -> handler returns 204 as before
  -> handler notifies hub: member left(room, member, left_at)
  -> hub sends member.left to remaining room connections
  -> hub closes/removes the leaving member's active connection if present
```

### Heartbeat

```text
server heartbeat loop -> heartbeat.ping(seq?, ping_id, server_time)
client -> heartbeat.pong(ping_id)
missing/invalid pong until timeout -> remove/close connection only
```

Heartbeat messages are JSON WebSocket messages, not WebSocket protocol ping frames, because browser clients cannot observe protocol pings.

## Message envelope design

- Server event envelope:
  - `type`: contract event name
  - `seq`: monotonically increasing per product room event stream
  - `sent_at`: RFC3339 UTC time
  - `payload`: event-specific object
- Client command envelope:
  - `type`
  - optional `request_id`
  - `payload`
- `room.snapshot` is sequenced and `payload.last_seq` equals envelope `seq`.
- Join/leave events are sequenced.
- Heartbeat may use the same envelope form for consistency; if implementation keeps heartbeat outside the room event sequence, document the choice in code/tests and keep client-facing JSON contract stable.

## Error mapping

Pre-upgrade mapping must match `docs/api/websocket.md`:

| Condition | Status | Code | Message |
| --- | --- | --- | --- |
| Missing/malformed/tampered/unsupported-version token | 401 | `invalid_room_session` | `连接凭证无效，请重新进入房间` |
| Expired token | 401 | `room_session_expired` | `连接凭证已过期，请重新进入房间` |
| Token room mismatch | 403 | `room_session_mismatch` | `连接凭证与房间不匹配` |
| Missing/disconnected/non-active member | 403 | `member_not_active` | `成员不在房间中` |
| Missing room | 404 | `room_not_found` | `房间不存在或已失效` |
| Expired room | 410 | `room_expired` | `该房间已过期，请让朋友重新创建` |
| Unexpected failure | 500 | `internal_error` | `服务器错误` |

Established connection errors:

- Unknown message type -> `room.error` with `unknown_message_type`.
- Invalid JSON/envelope -> `room.error` with `invalid_message` if recoverable, otherwise safe protocol close.
- Unsupported out-of-scope known commands (`member.mute_changed`, `member.speaking_changed`, etc.) should not mutate state in Issue #15; return `unknown_message_type` or a clear unsupported error if documented locally.

## Compatibility

- Existing HTTP responses and status codes must not change.
- Existing room/session/store package tests must keep passing.
- `docs/api/websocket.md` remains a broader MVP contract; this task implements only the Issue #15 subset. If implementation comments or docs mention partial support, they must not redefine the full contract.
- No frontend changes are required; tests can use Go WebSocket clients.

## Security and privacy

- Never log or persist the `token` query parameter, room session token, LiveKit token, room session secret, LiveKit API secret, sensitive request bodies, or audio content.
- WebSocket identity comes from verified token claims and persisted room/member state only.
- Unknown client payload member IDs are ignored and cannot authorize state changes.
- Do not add account/friend semantics to `anonymous_id`.

## Rollback shape

- The change is additive: remove the `ws` package, router option, hub notifications, dependency, and tests to return to HTTP-only behavior.
- Because no durable schema migration is required unless snapshot reads need only repository methods, rollback should not require database migration.

## Risks

- Broadcast timing after HTTP response: tests should assert eventual event delivery without relying on arbitrary sleeps.
- `room.snapshot` consistency while join/leave happens concurrently: use repository reads and hub locks narrowly; avoid holding DB work while blocking on slow client writes.
- Slow clients can block fan-out: use per-connection send queue or deadline-based writes so one client does not stall a room.
- Coder WebSocket dependency may alter `go.mod` Go/toolchain requirements; validate with `go test -count=1 ./services/api/...`.
