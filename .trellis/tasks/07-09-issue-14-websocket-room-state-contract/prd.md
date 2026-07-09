# Issue 14 WebSocket 房间状态契约

## Goal

Define the echo room WebSocket contract so the later WebSocket hub and desktop room-state client can be implemented and tested without inventing protocol details. The contract must specify connection/authentication, message envelope, server-to-client events, client-to-server commands, heartbeat, error format, reconnect semantics, and MVP exclusions.

## Background and Confirmed Facts

- GitHub Issue #14 is `[S09] WebSocket 房间状态契约`; it is a documentation task under epic #3 and is no longer blocked because Issue #13 is closed.
- Issue #14 acceptance requires the contract to include connection/authentication, `room.snapshot`, member join/leave, mute changes, speaking changes, reconnect state, client heartbeat, error handling, and enough detail for later WebSocket implementation tests.
- Issue #14 explicitly excludes implementing the WebSocket hub, chat messages, and host-management events.
- `docs/api/websocket.md:1` currently exists but only lists endpoint and message names; it does not define payload schemas, error shape, heartbeat, or reconnect behavior.
- `design.md:425` states WebSocket messages are maintained separately in `docs/api/websocket.md` and lists the intended server/client message names.
- ADR `0019` establishes the boundary: HTTP handles command-style operations; WebSocket carries realtime product room state.
- ADR `0032` establishes that HTTP stays in `services/api/openapi.yaml`, while WebSocket message types and payload schemas are documented separately.
- ADR `0020` requires dropped members to keep member ID, list position, and room slot for a 30-second reconnect window; after that they are removed and empty-room retention may start.
- ADR `0015` requires speaking state to be client-detected, reported over WebSocket, throttled by the service, and not treated as LiveKit-authoritative product state.
- ADR `0016` requires mute to update local LiveKit media state and business room state, with mute taking priority over push-to-talk and free-talk.
- ADR `0021` and `services/api/internal/session/token.go:23` establish short-lived room session credentials containing version, room ID, member ID, and expiry.
- `services/api/internal/http/handlers.go:214` uses `Authorization: Bearer <room_session_token>` for HTTP LiveKit-token refresh, but browser WebSocket clients cannot set arbitrary `Authorization` headers; existing implementation planning expects `?token=` for room WebSocket connection.

## First-Principles Constraints

- A WebSocket server cannot trust client-supplied `member_id`, `anonymous_id`, or LiveKit presence as authorization; only a server-signed room session token can prove the member already joined the product room.
- Browser/WebView2 WebSocket clients can provide URL and subprotocols, not arbitrary request headers. Therefore the MVP room WebSocket must define a browser-compatible authentication transport.
- Product room state is not media state. LiveKit carries voice media; the business service owns room membership, mute, speaking, reconnect, and lifecycle state.
- Realtime state must be bounded and recoverable. A client that misses messages must be able to request a fresh `room.snapshot` instead of relying on historical replay.
- The MVP must remain lightweight: no chat, host management, account semantics, long-lived tokens, server audio mixing, or WebSocket RPC expansion.

## Requirements

- **R1 — Connection and auth:** Document `GET /v1/rooms/{room_id}/ws?token=<room_session_token>` over WSS in production, including token verification, room-path matching, active/reconnecting member authorization, and sensitive-token redaction expectations.
- **R2 — Message envelope:** Define a common JSON envelope with `type`, monotonically increasing `seq` for server events, `sent_at`, and `payload`; define client command envelope with `type`, optional `request_id`, and `payload`.
- **R3 — Snapshot:** Define `room.snapshot` payload containing room metadata, current member list, self member ID, stable member ordering, connection timing values, and current sequence state.
- **R4 — Member lifecycle events:** Define `member.joined`, `member.left`, `member.reconnecting`, `member.disconnected`, and `member.restored` payloads and when each is emitted.
- **R5 — Voice-state events:** Define `member.muted_changed` and `member.speaking_changed` payloads, including server authority, client optimistic UI allowance, and speaking throttling constraints.
- **R6 — Client commands:** Define `heartbeat.pong`, `member.mute_changed`, `member.speaking_changed`, `member.voice_mode_changed`, `member.leave_requested`, and `room.resync_requested` payloads; make clear that member identity is derived from the token, not trusted from payload.
- **R7 — Heartbeat:** Document application-level heartbeat behavior suitable for browser WebSocket clients, including ping interval, pong timeout, and disconnect/reconnect transition expectations.
- **R8 — Errors and closure:** Define `room.error`, pre-upgrade failure behavior, established-connection error behavior, public error codes, retryability hints, and close-code guidance.
- **R9 — Reconnect semantics:** Document 30-second reconnect window behavior for WebSocket disconnects, clearing speaking state, preserving list position/room slot, restoration, timeout removal, and relation to LiveKit token refresh.
- **R10 — Explicit exclusions:** State that Issue #14 does not implement a WebSocket hub, does not add chat, does not add host-management events, does not change HTTP/OpenAPI endpoints, and does not make LiveKit authoritative for product room state.

## Acceptance Criteria

- [ ] `docs/api/websocket.md` documents endpoint, production WSS requirement, and query-token authentication with redaction notes.
- [ ] `docs/api/websocket.md` defines common server and client JSON message envelopes.
- [ ] `docs/api/websocket.md` defines payload schemas and examples for `room.snapshot`, member join/leave, mute, speaking, reconnecting/restored/disconnected, room expiry, errors, resync, and heartbeat messages.
- [ ] `docs/api/websocket.md` explains client heartbeat and error handling sufficiently for later automated and manual tests.
- [ ] `docs/api/websocket.md` explains 30-second reconnect semantics and how they interact with room session token validity and LiveKit token refresh.
- [ ] The contract states identity spoofing protections: client payloads cannot choose `member_id`; server derives room/member from the room session token.
- [ ] The contract preserves MVP boundaries: no WebSocket hub implementation, no chat, no host-management events, no account semantics, no server-side media authority.
- [ ] Verification runs `git diff --check` and `go test -count=1 ./services/api/...`; any failure caused by the change is fixed before completion.
- [ ] `trellis-check` is run after implementation, and any reported failure is fixed and rechecked until passing.

## Out of Scope

- Implementing `services/api/internal/ws/**` or adding a live WebSocket route.
- Adding frontend WebSocket client code.
- Changing `services/api/openapi.yaml` unless an actual HTTP contract inconsistency is discovered while implementing the docs.
- Designing or implementing chat, private messages, host kick/revoke/close/transfer controls, reporting, bans, fixed rooms, or room passwords.
- Adding long-lived credentials, storing room session tokens, or treating LiveKit as the product-room state source.
