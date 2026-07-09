# echo Room WebSocket Contract

This document defines the MVP room-state WebSocket contract for echo. It is the contract for a later WebSocket hub implementation; this document does not implement a server runtime.

## Purpose and boundaries

The room WebSocket carries realtime product room state after a member has already created or joined a temporary room through HTTP.

Use HTTP for:

- creating a temporary room;
- joining by invite code;
- leaving through command APIs when no WebSocket is available;
- issuing or refreshing LiveKit tokens;
- health checks and other request/response operations.

Use the room WebSocket for:

- room snapshots;
- member join, leave, reconnecting, restored, and disconnected state;
- mute state;
- speaking state;
- lightweight heartbeat;
- resync requests;
- room-state errors.

The business service is authoritative for product room state. LiveKit is authoritative only for media transport. LiveKit participant presence, `anonymous_id`, and client-provided payload fields are not sufficient authorization for room-state changes.

## Non-goals

The MVP room WebSocket contract does not include:

- chat, private messages, or message history;
- host-management actions such as kick, revoke invite, close room, or transfer host;
- account, friend, report, ban, fixed-room, or password semantics;
- server-side audio mixing or audio payload transport;
- long-lived credentials or persisted WebSocket tokens;
- an RPC layer for arbitrary room commands.

## Endpoint and authentication

Production clients connect over WSS:

```text
GET /v1/rooms/{room_id}/ws?token=<room_session_token>
```

Local development may use `ws://` when TLS is not available.

`token` is the short-lived room session token returned by `POST /v1/rooms` or `POST /v1/rooms/join`. The query parameter is used because browser/WebView2 WebSocket clients cannot attach arbitrary `Authorization` headers during the WebSocket handshake.

The token query parameter is sensitive:

- application logs, access logs owned by this service, test fixtures, screenshots, and error messages must redact it;
- examples in this document use placeholders only;
- the server must not persist room session tokens or LiveKit tokens.

Before accepting a WebSocket connection, the server must:

1. require a non-empty `token` query parameter;
2. verify the room session token signature, version, and expiry;
3. require `claims.room_id` to equal `{room_id}` from the path;
4. load the persisted product room and member from `claims.room_id` and `claims.member_id`;
5. require the room to be active;
6. require the member state to be `online` or `reconnecting`;
7. derive the connection's room and member identity only from the verified token and persisted product state.

A new valid connection for the same room/member may replace a stale connection for that member.

### Pre-upgrade failures

When the server can reject before the WebSocket upgrade, it should use the same JSON error envelope as HTTP APIs:

```json
{
  "error": {
    "code": "invalid_room_session",
    "message": "连接凭证无效，请重新进入房间"
  }
}
```

Expected pre-upgrade status mapping:

| Condition | HTTP status | Error code | Message |
| --- | --- | --- | --- |
| Missing, malformed, tampered, unsupported-version token | `401` | `invalid_room_session` | `连接凭证无效，请重新进入房间` |
| Expired room session token | `401` | `room_session_expired` | `连接凭证已过期，请重新进入房间` |
| Token room does not match path room | `403` | `room_session_mismatch` | `连接凭证与房间不匹配` |
| Member is missing, disconnected, or otherwise not active | `403` | `member_not_active` | `成员不在房间中` |
| Product room is missing | `404` | `room_not_found` | `房间不存在或已失效` |
| Product room is expired | `410` | `room_expired` | `该房间已过期，请让朋友重新创建` |
| Unexpected server failure | `500` | `internal_error` | `服务器错误` |

## Message envelopes

All messages are JSON objects. Message `type` values are stable contract names. Unknown top-level fields must be ignored. Unknown client command types must not mutate room state; they should produce `room.error` with `unknown_message_type` when the connection can continue. Invalid envelopes should produce `room.error` when the connection can continue, or a policy/protocol close when the connection cannot continue safely.

### Server-to-client event envelope

```json
{
  "type": "member.speaking_changed",
  "seq": 42,
  "sent_at": "2026-07-09T12:00:00Z",
  "payload": {}
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `type` | string | yes | Event type. |
| `seq` | integer | yes | Server-assigned, monotonically increasing sequence for the product room event stream. |
| `sent_at` | RFC3339 UTC string | yes | Server send time. |
| `payload` | object | yes | Event payload. |

`seq` is not a replay guarantee. WebSocket transport preserves order while connected, and clients use `seq` to detect gaps and request a fresh `room.snapshot`. A `room.snapshot` is also sequenced; its payload `last_seq` must match the envelope `seq` so clients know the current stream position.

### Client-to-server command envelope

```json
{
  "type": "member.speaking_changed",
  "request_id": "client-request-1",
  "payload": {}
}
```

Fields:

| Field | Type | Required | Description |
| --- | --- | --- | --- |
| `type` | string | yes | Command type. |
| `request_id` | string | no | Client-generated opaque ID. Echoed by `room.error` when the error is related to a command. |
| `payload` | object | yes | Command payload. |

Clients must not send `seq`. The server derives room ID and member ID from the verified room session token, not from client payload fields.

## Shared schemas

### Room object

```json
{
  "room_id": "room_example",
  "name": "Duo Night",
  "invite_code": "K7M9Q2",
  "state": "active",
  "created_at": "2026-07-09T12:00:00Z",
  "last_empty_at": null,
  "expires_at": null
}
```

| Field | Type | Description |
| --- | --- | --- |
| `room_id` | string | Product temporary room ID. |
| `name` | string | Room display name. |
| `invite_code` | string | Six-character uppercase invite code. |
| `state` | `active` or `expired` | Product room lifecycle state. |
| `created_at` | RFC3339 UTC string | Creation time. |
| `last_empty_at` | RFC3339 UTC string or null | Last time the room became empty. |
| `expires_at` | RFC3339 UTC string or null | Empty-room expiry time. |

### Member object

```json
{
  "member_id": "mem_alice",
  "nickname": "Alice",
  "avatar_id": "avatar_07",
  "is_self": true,
  "is_host": true,
  "state": "online",
  "muted": false,
  "speaking": false,
  "voice_mode": "push_to_talk",
  "joined_at": "2026-07-09T12:00:01Z",
  "reconnect_until": null
}
```

| Field | Type | Description |
| --- | --- | --- |
| `member_id` | string | Server-issued member ID for the temporary room. |
| `nickname` | string | Display nickname. It is not unique. |
| `avatar_id` | string | Random avatar identifier. |
| `is_self` | boolean | True only for the receiving connection's own member. |
| `is_host` | boolean | True for the room creator. This is a label, not management permission. |
| `state` | `online` or `reconnecting` | Current product member state shown in member lists. |
| `muted` | boolean | Product mute state. Mute always prevents audio sending. |
| `speaking` | boolean | Product speaking indicator reported by clients and throttled by the server. |
| `voice_mode` | `push_to_talk` or `free_talk` | Last reported voice mode. It does not by itself mean audio is being sent. |
| `joined_at` | RFC3339 UTC string | Original member join time. |
| `reconnect_until` | RFC3339 UTC string or null | Reconnect deadline when `state` is `reconnecting`. |

Snapshots include `online` and `reconnecting` members. `disconnected` members are removed from the active member list after `member.disconnected`. Member projections are rendered for each receiving connection; `is_self` can differ by recipient even when the underlying member is the same.

### Error object

```json
{
  "error": {
    "code": "invalid_message",
    "message": "消息格式无效"
  },
  "request_id": "client-request-1",
  "retryable": false
}
```

| Field | Type | Description |
| --- | --- | --- |
| `error.code` | string | Stable public error code. |
| `error.message` | string | User-understandable Chinese message. |
| `request_id` | string or null | Client command ID when applicable. |
| `retryable` | boolean | Whether the client may retry the same action without rejoining. |

## Server-to-client messages

### `room.snapshot`

Sent immediately after a connection is accepted and after a valid `room.resync_requested` command.

Payload:

```json
{
  "room": {
    "room_id": "room_example",
    "name": "Duo Night",
    "invite_code": "K7M9Q2",
    "state": "active",
    "created_at": "2026-07-09T12:00:00Z",
    "last_empty_at": null,
    "expires_at": null
  },
  "self_member_id": "mem_alice",
  "members": [
    {
      "member_id": "mem_alice",
      "nickname": "Alice",
      "avatar_id": "avatar_07",
      "is_self": true,
      "is_host": true,
      "state": "online",
      "muted": false,
      "speaking": false,
      "voice_mode": "push_to_talk",
      "joined_at": "2026-07-09T12:00:01Z",
      "reconnect_until": null
    }
  ],
  "last_seq": 42,
  "heartbeat_interval_ms": 15000,
  "heartbeat_timeout_ms": 30000,
  "reconnect_window_ms": 30000
}
```

Rules:

- `members` order is stable by original join order.
- Speaking changes must not reorder the member list.
- The receiving member has exactly one `is_self: true` member.
- `last_seq` is the highest room event sequence included in this snapshot.

### `member.joined`

Broadcast after a new member successfully joins the product room.

Payload:

```json
{
  "member": {
    "member_id": "mem_bob",
    "nickname": "Bob",
    "avatar_id": "avatar_08",
    "is_self": false,
    "is_host": false,
    "state": "online",
    "muted": false,
    "speaking": false,
    "voice_mode": "push_to_talk",
    "joined_at": "2026-07-09T12:02:00Z",
    "reconnect_until": null
  }
}
```

### `member.left`

Broadcast after a member intentionally leaves the room through WebSocket or HTTP leave flow.

Payload:

```json
{
  "member_id": "mem_bob",
  "left_at": "2026-07-09T12:20:00Z"
}
```

The member is removed from the active member list. If the room becomes empty, the business service starts the 30-minute empty-room retention timer outside this WebSocket contract.

### `member.reconnecting`

Broadcast when the server detects that a member's product room connection has dropped and the member enters the 30-second reconnect window.

Payload:

```json
{
  "member_id": "mem_bob",
  "reconnect_until": "2026-07-09T12:20:30Z",
  "reconnect_window_ms": 30000
}
```

Rules:

- The member keeps the same list position and room slot.
- The member state becomes `reconnecting`.
- The server clears that member's `speaking` state and must broadcast `member.speaking_changed` with `speaking: false` if clients could otherwise still show the member as speaking.
- Mute and voice-mode product state are preserved.

### `member.restored`

Broadcast when the same authorized member is restored within the reconnect window.

Payload:

```json
{
  "member": {
    "member_id": "mem_bob",
    "nickname": "Bob",
    "avatar_id": "avatar_08",
    "is_self": false,
    "is_host": false,
    "state": "online",
    "muted": false,
    "speaking": false,
    "voice_mode": "push_to_talk",
    "joined_at": "2026-07-09T12:02:00Z",
    "reconnect_until": null
  },
  "restored_at": "2026-07-09T12:20:10Z"
}
```

The restored member keeps the same `member_id` and list position.

### `member.disconnected`

Broadcast when a member's reconnect window expires.

Payload:

```json
{
  "member_id": "mem_bob",
  "disconnected_at": "2026-07-09T12:20:30Z",
  "reason": "reconnect_timeout"
}
```

After this event, the member is removed from the active member list and the room slot is released.

### `member.muted_changed`

Broadcast after the server accepts a mute-state change for a room member.

Payload:

```json
{
  "member_id": "mem_alice",
  "muted": true,
  "changed_at": "2026-07-09T12:05:00Z"
}
```

Rules:

- The client should update its local LiveKit audio track immediately for self-mute safety.
- The client may optimistically update only its own mute UI before the broadcast arrives.
- The server broadcast is the authoritative product room state for member lists and reconciles optimistic self-UI.
- Muting also forces the member's effective audio send state to false.

### `member.speaking_changed`

Broadcast after the server accepts and throttles a speaking-state change.

Payload:

```json
{
  "member_id": "mem_alice",
  "speaking": true,
  "changed_at": "2026-07-09T12:05:02Z"
}
```

Rules:

- Speaking is a product UI signal, not a media permission signal.
- Clients detect their own speaking state from voice mode, mute state, push-to-talk state, and local microphone input activity.
- The client may optimistically update only its own speaking UI before the broadcast arrives.
- The server may drop duplicate or overly frequent speaking reports.
- The server broadcast is the authoritative product room state for member lists and reconciles optimistic self-UI.
- The server must force `speaking: false` when a member mutes, leaves, disconnects, or enters `reconnecting`.
- Clients may keep a visual speaking highlight briefly after `speaking: false` to avoid flicker, but this visual delay is not a server state.

### `room.expired`

Broadcast when the product room expires while the connection is active.

Payload:

```json
{
  "room_id": "room_example",
  "expired_at": "2026-07-09T12:30:00Z",
  "reason": "empty_retention_elapsed"
}
```

Clients should stop room-state interaction, disconnect media, and return to a clear room-expired UI state.

### `room.error`

Sent for recoverable command or protocol errors after the WebSocket has been established.

Payload:

```json
{
  "error": {
    "code": "invalid_message",
    "message": "消息格式无效"
  },
  "request_id": "client-request-1",
  "retryable": false
}
```

Expected established-connection error codes:

| Error code | Message | Retryable | Meaning |
| --- | --- | --- | --- |
| `invalid_message` | `消息格式无效` | false | Envelope or payload is invalid. |
| `unknown_message_type` | `消息类型无效` | false | `type` is not supported by this contract. |
| `rate_limited` | `操作过于频繁，请稍后重试` | true | Client sent too many state changes. |
| `room_resync_required` | `房间状态需要重新同步` | true | Client should send `room.resync_requested`. |
| `room_expired` | `该房间已过期，请让朋友重新创建` | false | Product room expired. |
| `member_not_active` | `成员不在房间中` | false | Member can no longer mutate room state. |
| `internal_error` | `服务器错误` | true | Unexpected server failure. |

### `room.resync_required`

Sent when the server cannot safely continue incremental state delivery.

Payload:

```json
{
  "reason": "sequence_gap",
  "last_seq": 42
}
```

Clients should send `room.resync_requested`. The server then sends `room.snapshot`.

### `heartbeat.ping`

Application-level heartbeat sent by the server. Browser clients cannot observe WebSocket protocol ping frames, so the MVP contract uses JSON heartbeat messages.

Payload:

```json
{
  "ping_id": "ping-42",
  "server_time": "2026-07-09T12:00:15Z"
}
```

Clients must respond with `heartbeat.pong` containing the same `ping_id`.

## Client-to-server messages

Client payloads must not include authoritative `member_id` fields. If a client includes `member_id`, the server must ignore it for authorization and state ownership.

### `heartbeat.pong`

Response to `heartbeat.ping`.

Payload:

```json
{
  "ping_id": "ping-42"
}
```

### `member.mute_changed`

Requests a mute-state change for the current member.

Payload:

```json
{
  "muted": true
}
```

Rules:

- The server derives the member from the verified room session token.
- The server validates that the member is still active.
- On success, the server broadcasts `member.muted_changed`.
- The client should still update the local LiveKit track immediately before waiting for the broadcast.

### `member.speaking_changed`

Reports the current member's speaking signal.

Payload:

```json
{
  "speaking": true
}
```

Rules:

- Clients should send transition changes, not continuous level-meter samples.
- Clients should avoid sending more than 10 speaking reports per second per connection.
- The server may throttle, coalesce, or ignore reports that do not change state.
- The server must ignore `speaking: true` when the member is muted, disconnected, reconnecting, or otherwise not allowed to send audio.

### `member.voice_mode_changed`

Reports the current member's selected voice mode.

Payload:

```json
{
  "voice_mode": "free_talk"
}
```

Valid values are `push_to_talk` and `free_talk`.

Rules:

- This state is product room state, but it does not by itself mean audio is being sent.
- Free-talk sending still requires explicit in-room user action and must obey mute, connection, and device availability rules.
- The MVP does not require a dedicated server-to-client voice-mode event. The latest value appears in `room.snapshot` and may be used by later UI state if needed.

### `member.leave_requested`

Requests that the current member leave the room.

Payload:

```json
{}
```

On success, the server broadcasts `member.left` and closes the leaving member's WebSocket with a normal close.

### `room.resync_requested`

Requests a fresh room snapshot.

Payload:

```json
{
  "last_seen_seq": 42,
  "reason": "client_detected_gap"
}
```

On success, the server sends `room.snapshot` to the requesting connection.

## Heartbeat behavior

Recommended MVP heartbeat settings:

| Setting | Value |
| --- | --- |
| `heartbeat_interval_ms` | `15000` |
| `heartbeat_timeout_ms` | `30000` |
| `reconnect_window_ms` | `30000` |

The server sends `heartbeat.ping` every `heartbeat_interval_ms`. The client sends `heartbeat.pong` with the same `ping_id`. If no valid pong is observed within `heartbeat_timeout_ms`, the server treats the WebSocket as dead and starts the reconnect flow.

Heartbeat failure is product connection failure only. LiveKit may have its own media reconnection behavior, but LiveKit state does not override product room reconnect rules.

## Reconnect semantics

When a WebSocket connection drops unexpectedly:

1. The server marks the member `reconnecting`.
2. The server preserves the member ID, original list position, and room slot for 30 seconds.
3. The server clears that member's speaking state.
4. The server broadcasts `member.reconnecting` and, if needed, `member.speaking_changed` with `speaking: false`.
5. The client may reconnect to the same endpoint with a valid room session token before `reconnect_until`.
6. If authorization succeeds and the reconnect window has not expired, the server restores the same member and broadcasts `member.restored`.
7. The restored connection receives `room.snapshot`.
8. If the reconnect window expires, the server broadcasts `member.disconnected`, releases the room slot, and starts empty-room retention if no `online` or `reconnecting` members remain.

Room session token validity still applies during reconnect. If the room session token expires during the reconnect window, the WebSocket handshake fails with `room_session_expired`; the client must re-enter the room flow instead of bypassing authorization.

LiveKit tokens are not sent over the WebSocket. If media needs a fresh token after reconnect, the client uses:

```http
POST /v1/rooms/{room_id}/livekit-token
Authorization: Bearer <room_session_token>
```

## Sequence and resync rules

- Server `seq` increases by one for each room event emitted by the WebSocket hub.
- A `room.snapshot` includes `last_seq`, the highest sequence included in that snapshot.
- If a client observes a sequence gap, it should send `room.resync_requested`.
- If the server drops queued messages because a client is too slow, it should send `room.resync_required` when possible, or close the connection if backpressure makes delivery unsafe.
- The server is not required to replay historical events for MVP.

## Close-code guidance

Use standard WebSocket close codes:

| Condition | Close code | Notes |
| --- | --- | --- |
| Client intentionally leaves | `1000` normal closure | Send/broadcast `member.left` first when possible. |
| Browser/app navigates away or process exits | `1001` going away | Server starts reconnect flow unless an explicit leave completed. |
| Invalid non-JSON or unsupported message shape | `1003` unsupported data | Use after `room.error` if the connection cannot continue. |
| Authorization or policy violation after upgrade | `1008` policy violation | Example: token no longer maps to an active member. |
| Server error or restart | `1011` internal error | Client may show reconnecting and retry within the reconnect window. |

## Security, privacy, and logging

- Do not log room session token plaintext, LiveKit token plaintext, API secrets, room session secrets, sensitive request bodies, or audio data.
- Redact the `token` query parameter from application logs and error messages.
- Do not put real token-shaped values in committed docs, tests, examples, or fixtures.
- WebSocket speaking messages carry boolean product state only; they must not carry audio samples, level-meter histories, recordings, or transcripts.
- WebSocket room state must not add account, friend, or cross-device identity semantics to `anonymous_id`.

## Later implementation test checklist

A later WebSocket implementation should include tests or manual checks for:

- successful connection with a valid room session token;
- rejection of missing, malformed, expired, tampered, wrong-room, missing-member, disconnected-member, missing-room, and expired-room credentials;
- initial `room.snapshot` shape and stable member ordering;
- `member.joined` and `member.left` broadcasts;
- mute command validation and `member.muted_changed` broadcast;
- speaking command throttling and `member.speaking_changed` broadcast;
- heartbeat ping/pong timeout behavior;
- reconnecting, restored, and disconnected transitions across the 30-second window;
- resync required/requested flow returning a new `room.snapshot`;
- no chat, host-management, token logging, or LiveKit-authoritative product state behavior.
