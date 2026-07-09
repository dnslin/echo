# WebSocket Room-State Guidelines

> Room-state WebSocket contracts for the echo API service.

---

## Scenario: Room snapshot, member broadcasts, and heartbeat

### 1. Scope / Trigger

- Trigger: adding or modifying backend room-state WebSocket behavior under `services/api/**`.
- Applies to:
  - `services/api/internal/ws/**` for WebSocket handshake, hub state, message envelopes, heartbeat, and broadcasts;
  - `services/api/internal/http/**` for route registration, query-token redaction, and join/leave event notification;
  - `services/api/internal/room/**` for product-member authorization;
  - `services/api/internal/store/**` for snapshot reads and active-member ordering;
  - `services/api/cmd/api/**` for runtime hub wiring;
  - `docs/api/websocket.md` as the product/API contract source.
- Echo MVP uses the API service as the authority for product room state. LiveKit participant presence is not a room-state authorization source.

### 2. Signatures

WebSocket endpoint:

```http
GET /v1/rooms/{room_id}/ws?token=<room_session_token>
```

Hub constructor and HTTP entrypoint:

```go
type Config struct {
    Authorizer          Authorizer
    SnapshotStore       SnapshotStore
    RoomSessionSecret   string
    Now                 func() time.Time
    HeartbeatInterval   time.Duration
    HeartbeatTimeout    time.Duration
    ReconnectWindow     time.Duration
    WriteTimeout        time.Duration
    ConnectionQueueSize int
    OriginPatterns      []string
    ResyncMinInterval   time.Duration
}

func NewHub(config Config) *Hub
func (h *Hub) ServeRoomHTTP(w http.ResponseWriter, r *http.Request, roomID string)
```

Hub dependencies:

```go
type Authorizer interface {
    AuthorizeMemberContext(ctx context.Context, input room.AuthorizeMemberInput) (room.AuthorizeMemberResult, error)
}

type SnapshotStore interface {
    FindRoomByID(ctx context.Context, roomID string) (domain.Room, error)
    ListRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) ([]domain.Member, error)
}
```

HTTP integration:

```go
type roomWebSocket interface {
    ServeRoomHTTP(w http.ResponseWriter, r *http.Request, roomID string)
}

type roomEventNotifier interface {
    NotifyMemberJoined(ctx context.Context, roomValue domain.Room, memberValue domain.Member)
    NotifyMemberLeft(ctx context.Context, roomValue domain.Room, memberValue domain.Member)
}

func WithRoomWebSocket(roomWebSocket roomWebSocket) RouterOption
func WithRoomEventNotifier(roomEventNotifier roomEventNotifier) RouterOption
```

Snapshot repository method:

```go
func (r *Repository) ListRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) ([]domain.Member, error)
```

### 3. Contracts

- The browser/WebView2 client passes the room session token as query parameter `token` because WebSocket handshakes cannot attach arbitrary `Authorization` headers.
- Cross-origin browser/WebView2 clients may connect only through an explicit bounded `OriginPatterns` allowlist; do not use wildcard origin acceptance as the default.
- Before upgrade, the server must:
  1. require a non-empty `token` query parameter;
  2. verify room session token signature, version, expiry, and claims with `session.Verify`;
  3. require token `room_id` to equal path `{room_id}`;
  4. call `AuthorizeMemberContext` using the verified token room/member IDs;
  5. require persisted room state `active` and member state `online` or `reconnecting`.
- WebSocket identity must come only from verified token claims and persisted product state. Ignore client-sent member IDs, request bodies, `anonymous_id`, and LiveKit presence for authorization.
- Router code must redact the `token` query parameter from Gin context, recovery, and logging surfaces, including `URL.RawQuery` and `RequestURI`, while passing the original request or extracted token to authentication logic.
- Accepted connections immediately receive `room.snapshot` as the first client-visible message. Registration must be atomic with the snapshot sequence position so a connection cannot miss a concurrent room broadcast after its snapshot position is chosen.
- `room.snapshot` payload includes:
  - `room.room_id`, `name`, `invite_code`, `state`, `created_at`, `last_empty_at`, `expires_at`;
  - `self_member_id` from verified claims;
  - active `members` only (`online`, `reconnecting`), ordered by `joined_at ASC`, then `id ASC`;
  - exactly one `is_self: true` for the receiving member;
  - `last_seq` equal to the envelope `seq`;
  - heartbeat/reconnect durations in milliseconds.
- Room-wide shared `seq` advances only for shared room broadcasts. Client-private snapshots, heartbeat pings, and room errors reuse the latest shared sequence visible to that receiver and must not create false sequence gaps for other clients.
- Successful HTTP join sends `member.joined` to already-connected clients in the same room. The joining member sees itself through its later snapshot.
- Successful HTTP leave sends `member.left` to connected clients in the same room and closes the leaving member's connection normally after the event when present.
- Heartbeat is application-level JSON: server sends `heartbeat.ping`, client sends `heartbeat.pong` with the same `ping_id`. Timeout removes/closes the transport only and must not write reconnect/disconnected product state.
- Repeated `room.resync_requested` messages from one connection must be bounded with a deterministic guard such as rate limiting or coalescing; normal single resync still returns a snapshot.
- Unsupported or invalid established-connection messages must not mutate room state; return `room.error` when recoverable or close safely.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| Missing `token` query parameter | Reject before upgrade with `401 invalid_room_session`. |
| Malformed, tampered, wrong-secret, unsupported-version, or missing-claims token | Reject before upgrade with `401 invalid_room_session`. |
| Expired room session token | Reject before upgrade with `401 room_session_expired`. |
| Token `room_id` does not match path `{room_id}` | Reject before upgrade with `403 room_session_mismatch`; do not load room/member state. |
| Persisted room is missing | Reject before upgrade with `404 room_not_found`. |
| Persisted room is expired | Reject before upgrade with `410 room_expired`. |
| Persisted member is missing or inactive/disconnected | Reject before upgrade with `403 member_not_active`. |
| Hub dependencies or room session secret are missing | Return `500 internal_error`; do not upgrade. |
| Browser/WebView2 Origin is not allowed by configured origin patterns | Reject before upgrade; do not bypass with wildcard defaults. |
| Snapshot cannot be built after upgrade | Send `room.error` if possible, then close with internal error. |
| Client sends invalid JSON or blank message type | Send recoverable `room.error` with `invalid_message`; do not mutate product state. |
| Client sends unknown/out-of-scope message type | Send recoverable `room.error` with `unknown_message_type`; do not mutate product state. |
| Missing matching `heartbeat.pong` before timeout | Close/remove connection; do not change persisted member state. |
| Burst `room.resync_requested` from one connection | Bound snapshot work and return retryable `room.error` with `rate_limited` or equivalent deterministic guard. |
| Send queue is full for a slow client | Close/remove connection safely so one client cannot block room fan-out. |

### 5. Good/Base/Bad Cases

- Good: WebSocket handshake verifies the query token, checks path-room match, authorizes persisted product room/member state, upgrades, queues the initial snapshot, atomically registers the connection at that snapshot position, then starts read/write/heartbeat loops.
- Good: `POST /v1/rooms/join` remains the only product join mutation path; after success, the handler notifies the hub to broadcast `member.joined`.
- Good: `POST /v1/rooms/{room_id}/leave` remains the only product leave mutation path; after bearer-token authorization proves the caller is leaving itself, success notifies the hub to broadcast `member.left` and close the leaving connection.
- Base: snapshots are projections rebuilt from SQLite room/member state plus the receiving member ID; they are not persisted as separate state and do not advance room-wide shared sequence by themselves.
- Base: room sequence counters, WebSocket connections, heartbeat pings, resync guards, and send queues are in-memory single-instance transport state; empty room connection state should be pruned.
- Bad: accepting any `member_id` from a WebSocket client payload as authorization.
- Bad: deriving room membership from LiveKit participants.
- Bad: registering a connection for room broadcasts before its initial snapshot has been queued, or building a snapshot and only registering later so concurrent broadcasts are missed.
- Bad: logging raw WebSocket URLs that contain `?token=...` through either `URL.RawQuery` or `RequestURI`.
- Bad: letting one member close another member's WebSocket through unauthenticated HTTP leave.
- Bad: allowing unlimited resync commands to force unbounded snapshot reads/serialization.
- Bad: implementing mute, speaking, reconnect product-state transitions, chat, or cross-instance fan-out inside the snapshot/broadcast task.

### 6. Tests Required

- WebSocket integration tests:
  - valid token cross-origin connection succeeds only when the Origin is explicitly allowed;
  - valid token connection receives immediate `room.snapshot`;
  - a connection cannot miss a room event between snapshot position selection and registration;
  - private snapshots, heartbeat pings, and room errors do not advance shared sequence for other clients;
  - missing, malformed, tampered, unsupported-version, expired, and wrong-room tokens are rejected with the documented status/code/message;
  - missing room, expired room, missing member, and disconnected member are rejected before upgrade;
  - snapshot members are ordered by original join time with stable ID tie-breaker, exclude disconnected members, and set exactly one `is_self`;
  - at least two already-connected clients receive `member.joined` after a later HTTP join;
  - HTTP leave broadcasts `member.left`, closes/removes the leaving connection when present, and later snapshots exclude the member;
  - heartbeat ping/pong keeps the connection usable;
  - heartbeat timeout closes/removes the connection without product-state mutation;
  - burst `room.resync_requested` messages are bounded while a normal single resync succeeds;
  - empty per-room hub state is pruned after the last connection closes and HTTP notifications without clients do not retain rooms;
  - unknown and invalid client messages return `room.error` or safe close without mutation.
- HTTP/router tests:
  - WebSocket route is registered only when `WithRoomWebSocket` is configured;
  - redacted request preserves non-sensitive query fields, leaves the original request unchanged, redacts both `URL.RawQuery` and `RequestURI`, and still passes the original token to the hub;
  - authenticated HTTP leave cannot close/broadcast leave for another member when the bearer token member differs from the body `member_id`.
- Store tests:
  - `ListRoomMembersByStates` returns only requested states, orders by `joined_at ASC`, then `id ASC`, and returns empty for empty state filters.
- Runtime wiring tests:
  - env-backed router can create a room and open the WebSocket snapshot path.
- Full backend check:

```bash
go test -count=1 ./services/api/...
git diff --check
```

### 7. Wrong vs Correct

#### Wrong

```go
func (h *Hub) ServeRoomHTTP(w http.ResponseWriter, r *http.Request, roomID string) {
    memberID := r.URL.Query().Get("member_id")
    // Wrong: client-controlled identity, no token verification, no persisted product authorization.
    conn, _ := websocket.Accept(w, r, nil)
    h.register(h.newConnection(conn, roomID, memberID))
}
```

Why wrong: `member_id` is client-controlled and is not proof of a successful room join. This lets a client impersonate another member and bypass current room/member state.

#### Correct

```go
claims, err := session.Verify(session.VerifyInput{
    Secret: h.roomSessionSecret,
    Token: token,
    Now: h.currentTime(),
})
if err != nil {
    writeSessionHTTPError(w, err)
    return
}
if claims.RoomID != strings.TrimSpace(roomID) {
    writeHTTPError(w, http.StatusForbidden, "room_session_mismatch", "连接凭证与房间不匹配")
    return
}
authorized, err := h.authorizer.AuthorizeMemberContext(r.Context(), room.AuthorizeMemberInput{
    RoomID: claims.RoomID,
    MemberID: claims.MemberID,
})
if err != nil {
    writeAuthorizeHTTPError(w, err)
    return
}
```

Why correct: the bearer credential proves server-issued room/member claims, and the room service verifies current persisted product state before any WebSocket payload is trusted.

#### Wrong

```go
v1.GET("/rooms/:room_id/ws", func(c *gin.Context) {
    config.roomWebSocket.ServeRoomHTTP(c.Writer, c.Request, c.Param("room_id"))
})
```

Why wrong: Gin recovery or request logging surfaces can observe the raw URL containing `?token=...`.

#### Correct

```go
v1.GET("/rooms/:room_id/ws", func(c *gin.Context) {
    originalRequest := c.Request
    c.Request = redactedTokenRequest(originalRequest)
    config.roomWebSocket.ServeRoomHTTP(c.Writer, originalRequest, c.Param("room_id"))
    c.Request = originalRequest
})
```

Why correct: logging/recovery surfaces see the redacted request, while the authentication path still receives the original token.
