# Design — Issue 14 WebSocket 房间状态契约

## Purpose

Expand `docs/api/websocket.md` from a message-name placeholder into a concrete, testable room-state WebSocket contract for echo MVP.

## Scope Boundary

This task changes documentation only. It must not implement the WebSocket hub, route, frontend client, or persistence changes. The document is the source of truth for later WebSocket implementation tests.

## Architecture Boundary

- HTTP command APIs remain documented in `services/api/openapi.yaml`.
- Room WebSocket event streams are documented in `docs/api/websocket.md`.
- The business service is authoritative for product room state.
- LiveKit remains authoritative only for media transport.
- Room session tokens authorize WebSocket membership; `anonymous_id`, request payload `member_id`, and LiveKit participant presence are not authorization.

## Authentication Design

Production connection:

```text
GET /v1/rooms/{room_id}/ws?token=<room_session_token>
```

Rationale from first principles:

1. The client runs in React/WebView2 browser APIs.
2. Browser WebSocket constructors cannot attach arbitrary `Authorization` headers.
3. A room session token must reach the server before room messages are accepted.
4. The simplest browser-compatible MVP transport is a query parameter.
5. Because query strings may appear in infrastructure logs, the contract must mark `token` as sensitive and require redaction from application logs and examples.

Token verification mirrors the credential contract:

- parse and verify HMAC room session token;
- reject expired, malformed, tampered, unsupported-version tokens;
- require token room ID to match `{room_id}`;
- authorize persisted member state as `online` or `reconnecting`;
- reject expired/missing room or disconnected/missing member.

## Message Envelope Design

Server event envelope:

```json
{
  "type": "member.speaking_changed",
  "seq": 42,
  "sent_at": "2026-07-09T12:00:00Z",
  "payload": {}
}
```

Client command envelope:

```json
{
  "type": "member.speaking_changed",
  "request_id": "client-generated-id",
  "payload": {}
}
```

`seq` is server-assigned per room connection stream and allows clients to detect missed state and request `room.snapshot`. WebSocket/TCP preserves order while connected, so this is not a replay-log design.

## Payload Model

Shared member projection should include enough UI state for MVP:

- `member_id`
- `nickname`
- `avatar_id`
- `is_self`
- `is_host`
- `state`: `online | reconnecting`
- `muted`
- `speaking`
- `voice_mode`: `push_to_talk | free_talk`
- `joined_at`
- optional `reconnect_until`

Room projection should include:

- `room_id`
- `name`
- `invite_code`
- `state`
- optional lifecycle timestamps already exposed by HTTP room responses.

## Event Semantics

- `room.snapshot`: sent immediately after successful connection and after valid resync request.
- `member.joined`: new active member joined after invite validation.
- `member.left`: member intentionally left.
- `member.reconnecting`: WebSocket/product connection dropped; server clears speaking and preserves slot for 30 seconds.
- `member.restored`: same member restored within reconnect window.
- `member.disconnected`: reconnect window elapsed; member slot released.
- `member.muted_changed`: member mute product state changed.
- `member.speaking_changed`: client-detected speaking product signal changed after server validation/throttling.
- `room.expired`: room expired; clients should leave room context.
- `room.error`: structured product/protocol error.
- `room.resync_required`: server cannot safely apply incremental state and asks client to request snapshot.
- `heartbeat.ping`: application-level ping because browser clients do not expose WebSocket protocol pings.

Client commands:

- `heartbeat.pong`: response to server ping.
- `member.mute_changed`: current member requests mute state change.
- `member.speaking_changed`: current member reports speaking signal.
- `member.voice_mode_changed`: current member reports selected voice mode; MVP does not require a dedicated server broadcast event.
- `member.leave_requested`: current member asks to leave.
- `room.resync_requested`: client asks for full snapshot.

## Heartbeat and Reconnect

- Server sends application `heartbeat.ping` at a documented interval, recommended 15 seconds.
- Client responds with `heartbeat.pong` carrying the same `ping_id`.
- If the server considers the connection dead, it marks the member `reconnecting`, clears speaking, broadcasts `member.reconnecting`, and starts the 30-second reconnect window.
- If the same authorized member reconnects before the window ends, server emits `member.restored` and sends a fresh `room.snapshot` to that connection.
- If the window expires, server emits `member.disconnected`, releases capacity, and may start 30-minute empty-room retention if no active/reconnecting members remain.
- LiveKit media reconnection still uses the HTTP LiveKit-token refresh path; WebSocket reconnect does not embed LiveKit tokens.

## Error Design

Error payload follows the existing API envelope shape:

```json
{
  "type": "room.error",
  "seq": 43,
  "sent_at": "2026-07-09T12:00:01Z",
  "payload": {
    "error": {
      "code": "invalid_message",
      "message": "消息格式无效"
    },
    "request_id": "optional-client-request-id",
    "retryable": false
  }
}
```

Pre-upgrade authentication failures can be expressed as HTTP status with the existing JSON error shape when the server implementation can still return HTTP. Established-connection failures use `room.error` and, when unrecoverable, close the WebSocket with documented close-code guidance.

## Compatibility and Migration

- Existing HTTP OpenAPI remains unchanged for this task.
- Existing placeholder message names in `docs/api/websocket.md` should be preserved and expanded, not renamed without source-document justification.
- The contract should be written in English to match `.trellis/spec/backend/index.md` documentation language guidance.

## Risks and Controls

- Query-token leakage risk: mitigate by documenting redaction and using placeholder token examples only.
- Over-broad protocol risk: mitigate by keeping client commands limited to current MVP room state and excluding chat/host-management/RPC semantics.
- Drift risk: include a later implementation checklist requiring hub tests to map directly to each documented message.
