# WebSocket room snapshot and member broadcasts

## Goal

Implement the MVP backend room-state WebSocket for Issue #15 so an already-created or joined room member can connect with a valid room session token, immediately receive the current product room snapshot, and see other members join or intentionally leave in realtime.

User value: after entering a temporary voice room, friends can trust the member list as the live product-room state instead of refreshing HTTP responses manually.

## Source issue

- GitHub Issue #15: `[S10] WebSocket 房间快照和成员加入/离开广播`
- Parent epic: Issue #3
- Blocked-by issue #14 is closed and delivered `docs/api/websocket.md` as the room-state WebSocket contract.

## Confirmed facts

- Product scope is a Windows voice-chat MVP for 2-10 friends in a temporary room; members must see the member list and later voice states in room UI (`prd.md:86-105`, `prd.md:111-123`).
- Business service owns product rooms, invite codes, membership, lifecycle, room session tokens, and WebSocket state; LiveKit owns only media transport (`design.md:80-88`, `design.md:248-259`).
- WebSocket endpoint contract is `GET /v1/rooms/{room_id}/ws?token=<room_session_token>` because browser/WebView2 clients cannot attach arbitrary handshake headers (`docs/api/websocket.md:39-55`).
- Handshake must verify token signature/version/expiry, path-room match, active product room, and member state `online` or `reconnecting`; identity must derive only from verified token plus persisted product state (`docs/api/websocket.md:57-68`).
- Issue #15 requires valid-session-only connection, initial room snapshot, member join broadcast, member leave broadcast, rejection of invalid/expired/non-member credentials, basic heartbeat, and multi-client integration tests.
- Issue #15 explicitly excludes mute, speaking, reconnect state, chat, and cross-instance broadcasting.
- Existing backend already has create/join/leave APIs, room session HMAC tokens, LiveKit token issuance, active-member authorization, and SQLite-backed room/member persistence (`services/api/internal/http/handlers.go:121-243`, `services/api/internal/session/token.go:43-117`, `services/api/internal/room/service.go:276-317`, `services/api/internal/store/sqlite.go:78-104`).
- Existing router exposes HTTP routes via option injection but has no WebSocket route yet (`services/api/internal/http/router.go:48-75`).
- Existing `go.mod` does not currently include `github.com/coder/websocket`; Issue #15 implementation may add the dependency for the planned hub (`services/api/go.mod:4-9`, `implement.md:8-9`).
- Existing durable `members` rows preserve original join order through `joined_at`; snapshots must order members by original join order as required by the contract (`services/api/internal/store/models.go:23-35`, `docs/api/websocket.md:263-269`).

## Requirements

### R1. WebSocket endpoint and handshake authentication

- Add `GET /v1/rooms/:room_id/ws` to the API router when the room-state WebSocket hub is configured.
- Require `token` query parameter; do not accept body fields, `anonymous_id`, LiveKit identity, or client-provided member fields as authorization.
- Verify the room session token using the existing `session.Verify` contract and configured room session secret.
- Reject missing, malformed, tampered, unsupported-version, and expired tokens before upgrade when possible.
- Reject token room/path mismatch before upgrade.
- Load product room/member through existing room authorization behavior and reject missing room, expired room, missing member, disconnected member, or otherwise inactive member before upgrade when possible.
- Use the same JSON error envelope shape and public error codes/messages defined by `docs/api/websocket.md` for pre-upgrade failures.
- Redact or avoid logging the `token` query parameter; tests and fixtures must not contain real token-shaped credential values beyond deterministic test-only strings already produced inside tests.

### R2. Initial room snapshot

- After accepting a valid WebSocket connection, immediately send `room.snapshot`.
- Snapshot payload must include:
  - room object fields required by `docs/api/websocket.md`;
  - `self_member_id` matching the verified member;
  - `members` containing active members (`online` and `reconnecting`) ordered by original join time;
  - exactly one `is_self: true` projection for the receiving connection;
  - `is_host`, `muted`, `speaking`, `voice_mode`, `joined_at`, and `reconnect_until` fields with contract-compatible values;
  - `last_seq` equal to the event envelope `seq` for that snapshot;
  - `heartbeat_interval_ms = 15000`, `heartbeat_timeout_ms = 30000`, and `reconnect_window_ms = 30000`.
- Snapshot must not include disconnected members.

### R3. Member joined broadcast

- When a new member successfully joins the product room through the existing HTTP join flow, all already-connected WebSocket clients in that room must receive `member.joined`.
- The newly joined member must be represented with `is_self: false` for existing receivers.
- The join event must use the same room event sequence stream as snapshots.
- The implementation must not depend on LiveKit participant presence to decide or authorize product membership.

### R4. Member left broadcast

- When a member intentionally leaves the product room through the existing HTTP leave flow, connected clients in that room must receive `member.left` with the leaving `member_id` and `left_at`.
- The leaving member must be removed from subsequent snapshots and active member lists.
- If the leaving member has an active WebSocket connection, it may be closed normally after the event is sent; remaining clients must keep receiving room-state events.
- Existing empty-room retention semantics must remain owned by the room service/store.

### R5. Basic heartbeat

- Accepted WebSocket connections must receive application-level `heartbeat.ping` JSON messages at the configured interval.
- Clients must be able to respond with `heartbeat.pong` containing the same `ping_id` without mutating product room state.
- If a client does not send a valid pong within the timeout, the server must close or mark the connection dead for the hub without adding reconnect/disconnected product-state behavior in this issue.
- Heartbeat handling must not send or receive audio data.

### R6. Scope constraints

- Do not implement mute-state commands or `member.muted_changed`.
- Do not implement speaking-state commands or `member.speaking_changed`.
- Do not implement reconnecting/restored/disconnected product-state transitions.
- Do not implement chat, private messages, host-management actions, accounts, friends, cross-instance pub/sub, Redis, or distributed broadcasting.
- Do not add frontend UI or desktop app behavior in this issue.

## Acceptance criteria

- [ ] AC1: `GET /v1/rooms/{room_id}/ws?token=<room_session_token>` is registered and accepts only valid active room-member credentials.
- [ ] AC2: Missing token, malformed/tampered token, expired token, wrong-room token, missing room, expired room, missing member, and disconnected/non-active member are rejected with the status/code/message matrix from `docs/api/websocket.md`.
- [ ] AC3: A successful connection receives `room.snapshot` immediately, with sequenced envelope fields and contract-compatible room/member payloads.
- [ ] AC4: Snapshot member list includes online/reconnecting members in original join order, excludes disconnected members, and sets exactly one receiver-specific `is_self` flag.
- [ ] AC5: With at least two connected WebSocket clients in one room, a later HTTP join broadcasts `member.joined` to the existing clients.
- [ ] AC6: With multiple connected clients, an HTTP leave broadcasts `member.left` to the remaining clients and subsequent snapshots exclude the leaving member.
- [ ] AC7: Basic heartbeat sends `heartbeat.ping`; a matching `heartbeat.pong` keeps the connection usable; missing pong eventually closes or removes the connection without product reconnect-state events.
- [ ] AC8: Unknown or invalid established-connection client messages do not mutate room state and produce `room.error` or a safe protocol close consistent with `docs/api/websocket.md`.
- [ ] AC9: WebSocket integration tests cover valid connection, pre-upgrade rejection cases, initial snapshot, multi-client join broadcast, multi-client leave broadcast, and heartbeat ping/pong.
- [ ] AC10: Existing HTTP create/join/leave/session/LiveKit tests still pass.
- [ ] AC11: `docs/api/websocket.md` remains the contract source; if implementation intentionally narrows Issue #15 behavior, the document notes this issue's supported subset without contradicting the full later contract.
- [ ] AC12: No room session token plaintext, LiveKit token plaintext, room session secret, LiveKit secret, sensitive request body, or audio data is logged or committed in docs/fixtures.

## Out of scope

- Frontend `roomSocket.ts`, reducer, UI member-list changes, LiveKit client integration, and desktop reconnect behavior.
- Mute/speaking/voice-mode server commands and broadcasts.
- Reconnect window state transitions (`member.reconnecting`, `member.restored`, `member.disconnected`) and related persistence updates.
- Room expiry broadcasts, resync-required/replay queues, slow-consumer backpressure beyond safe close, cross-instance fan-out, Redis, and horizontal scaling.
- OpenAPI WebSocket schema generation; WebSocket contract remains in `docs/api/websocket.md`.

## Open questions

None blocking after repository inspection. Issue #15 plus the closed #14 contract define enough scope for implementation planning.
