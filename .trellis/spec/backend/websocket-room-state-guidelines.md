# WebSocket Room-State Guidelines

> Room-state WebSocket contracts for the echo API service.

---

## Scenario: Room snapshot, member broadcasts, reconnect, and heartbeat

### 1. Scope / Trigger

- Trigger: adding or modifying backend room-state WebSocket behavior under `services/api/**`.
- Applies to:
  - `services/api/internal/ws/**` for WebSocket handshake, per-room transition serialization, hub state, message envelopes, heartbeat, reconnect, and broadcasts;
  - `services/api/internal/http/**` for route registration, query-token redaction, and join/leave event notification;
  - `services/api/internal/room/**` for product-member authorization and committed member-transition results;
  - `services/api/internal/store/**` for snapshot reads, active-member ordering, and atomic mute/speaking/lifecycle transitions;
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
    StateMutator        StateMutator
    RoomSessionSecret   string
    Now                 func() time.Time
    HeartbeatInterval   time.Duration
    HeartbeatTimeout    time.Duration
    ReconnectWindow     time.Duration
    WriteTimeout        time.Duration
    ConnectionQueueSize int
    OriginPatterns      []string
    ResyncMinInterval   time.Duration
    SpeakingMinInterval time.Duration
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

type StateMutator interface {
    UpdateMemberMuteContext(ctx context.Context, input room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error)
    UpdateMemberSpeakingContext(ctx context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error)
    DisconnectMemberContext(ctx context.Context, input room.DisconnectMemberInput) (room.LeaveResult, error)
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

Snapshot and durable transition repository methods:

```go
func (r *Repository) ListRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) ([]domain.Member, error)
func (r *Repository) UpdateMemberMute(ctx context.Context, roomID string, memberID string, muted bool) (domain.MemberMuteTransition, error)
func (r *Repository) UpdateMemberSpeaking(ctx context.Context, roomID string, memberID string, speaking bool) (domain.MemberSpeakingTransition, error)
func (r *Repository) LeaveRoomMember(ctx context.Context, roomID string, memberID string, activeStates []domain.MemberState, leftAt time.Time, retention time.Duration) (domain.MemberDisconnectTransition, error)

type MemberMuteTransition struct {
    Member          Member
    MutedChanged    bool
    SpeakingChanged bool
}

type MemberSpeakingTransition struct {
    Member  Member
    Changed bool
}

type MemberDisconnectTransition struct {
    Room         Room
    Member       Member
    Transitioned bool
}
```

These are internal service/store contracts. They do not add fields or message types to the public HTTP or WebSocket protocol.

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
- Every operation that selects a snapshot position or changes shared state for one room must run through that room's transition gate: initial/restored registration, private resync snapshots, HTTP join/leave notification, mute/speaking commands, transport loss, reconnect retry, and reconnect timeout. Pin the room state before waiting for the gate so pruning cannot detach an in-flight operation from the map.
- The Hub-wide mutex may protect only short room-map, connection-map, sequence, reconnect-record, throttle, and queue mutations. Release it before waiting on a room gate, calling an authorizer/store/state mutator, serializing a snapshot, scheduling/stopping timers, or closing a WebSocket. While holding a room gate, brief Hub-mutex sections are allowed; external I/O is not.
- Accepted connections immediately receive `room.snapshot` as the first client-visible message. Registration must be atomic with the snapshot sequence position so a connection cannot miss a concurrent room broadcast after its snapshot position is chosen.
- `room.snapshot` payload includes:
  - `room.room_id`, `name`, `invite_code`, `state`, `created_at`, `last_empty_at`, `expires_at`;
  - `self_member_id` from verified claims;
  - active `members` only (`online`, `reconnecting`), ordered by `joined_at ASC`, then `id ASC`;
  - exactly one `is_self: true` for the receiving member;
  - `last_seq` equal to the envelope `seq`;
  - heartbeat/reconnect durations in milliseconds.
- Room-wide shared `seq` advances once for each shared envelope, including each envelope inside an ordered logical group. Client-private snapshots, heartbeat pings, and room errors reuse the latest shared sequence visible to that receiver and must not create false sequence gaps for other clients.
- A connection send queue contains ordered logical groups, not individual envelopes. One group may contain `room.snapshot` then `member.restored`, `member.speaking_changed(false)` then `member.reconnecting`, or `member.speaking_changed(false)` then `member.muted_changed`. `ConnectionQueueSize` limits groups; a genuinely full finite group queue still triggers slow-consumer removal.
- Successful HTTP join sends `member.joined` to already-connected clients in the same room. The joining member sees itself through its later snapshot.
- Successful HTTP leave sends `member.left` and closes the leaving connection only when the committed lifecycle result has `Transitioned = true`. An idempotent or competing terminal operation with `Transitioned = false` emits no terminal event.
- Established-connection commands in this scope are `heartbeat.pong`, `room.resync_requested`, `member.mute_changed`, and `member.speaking_changed`. Member identity derives only from verified connection claims. Before durable mutation and again before publication, the connection must still be the current registered owner of its room/member slot; a detached or replaced connection cannot publish a stale result.
- Mute and speaking eligibility checks plus writes must run in one repository-owned `BEGIN IMMEDIATE` transaction. The transaction reloads the room/member, requires an active room and an `online` member, applies the requested transition, and returns the committed member and change flags. The room service and Hub must not reconstruct authoritative state from an earlier authorization read.
- Every committed member row must satisfy `muted => !speaking`. Muting clears speaking atomically, a muted member cannot commit `speaking=true`, and `UpdateMemberSpeaking` must repair a historical `muted=true/speaking=true` row to `speaking=false` even when the requested value would otherwise be a no-op.
- Accepted `member.mute_changed` broadcasts only events represented by the committed change flags and makes later snapshots reflect the committed member. If muting also clears speaking, both shared envelopes are delivered as one ordered group.
- Accepted `member.speaking_changed` treats speaking as a client-reported UI signal only. The server must not do audio detection, must dedupe/throttle repeated transitions deterministically, must not reorder members, and must make later snapshots reflect the committed state.
- Only an accepted client-originated speaking transition may advance `lastSpeakingAccepted`. Server-enforced `speaking=false` caused by mute, transport loss, reconnect cleanup, leave, or disconnect must delete or preserve—not advance—the throttle entry. Permanent leave/disconnect removes the entry.
- Unexpected transport loss detaches the connection and hands off lifecycle ownership before any potentially blocking WebSocket close handshake. It records one fixed reconnect deadline measured from loss detection and starts in internal phase `pending`; retry attempts must never move that deadline.
- Reconnect publication is durable-first. While `pending`, the Hub must durably clear speaking before broadcasting `member.speaking_changed(false)` or `member.reconnecting`; success moves the record to `restorable`. A transient failure keeps the record and capacity, publishes no authoritative reconnect transition, and retries with a bounded per-attempt context and bounded delay without an attempt-count cutoff.
- At or after the original deadline, a reconnect record becomes `timing_out` and cannot be restored, even if durable cleanup is still retrying. Terminal disconnect attempts continue until durable success or a stable competing terminal outcome; retries retain the original deadline.
- Reconnect registration must re-authorize current durable member state inside the room transition boundary and re-check the in-memory phase and deadline at the registration linearization point. Only an unexpired `restorable` record may restore. The internal `pending`, `restorable`, and `timing_out` phases are not public payload fields.
- Successful reconnect preserves the original member ID and list position, queues `room.snapshot` as the restored connection's first client-visible message, registers it at that snapshot position, then broadcasts exactly one `member.restored`. The restored member comes from fresh authoritative data and is projected per recipient so only that recipient's own member has `is_self=true`.
- HTTP leave and reconnect timeout use the committed `Transitioned` result as the single terminal winner. Only the winner broadcasts `member.left` or `member.disconnected`; the loser removes obsolete local reconnect state without broadcasting, and capacity is released only after durable disconnect succeeds.
- Heartbeat is application-level JSON: server sends `heartbeat.ping`, client sends `heartbeat.pong` with the same `ping_id`. Timeout closes the transport, enters the reconnect flow, and must not durably disconnect the member until the reconnect window expires.
- Repeated `room.resync_requested` messages from one connection must be bounded with a deterministic guard such as rate limiting or coalescing; normal single resync still returns a snapshot through the room transition boundary.
- Unsupported or invalid established-connection messages must not mutate room state; return `room.error` when recoverable or close safely.
- Failed initial snapshot/registration and permanent terminal paths must release room pins, timers, reconnect records, and member throttle entries. An otherwise empty per-room Hub state must be pruned.

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
| Snapshot cannot be built after upgrade | Send `room.error` if possible, close with internal error, release the room pin, and prune an otherwise empty room state. |
| Client sends invalid JSON or blank message type | Send recoverable `room.error` with `invalid_message`; do not mutate product state. |
| Client sends unknown/out-of-scope message type | Send recoverable `room.error` with `unknown_message_type`; do not mutate product state. |
| Room/member becomes ineligible before a mute/speaking commit | The immediate transaction returns the existing mapped room/member error; do not write or broadcast. |
| Historical row has `muted=true` and `speaking=true` | The transition must commit a row satisfying `muted => !speaking`; `UpdateMemberSpeaking` repairs it to `muted=true/speaking=false` even for an otherwise no-op request. |
| Command connection is no longer current before mutation | Do not call the durable mutator and do not broadcast. |
| Command connection is detached/replaced before publication | Suppress the stale publication; normal room serialization must prevent this interleaving from overtaking lifecycle events. |
| Missing matching `heartbeat.pong` before timeout | Detach the transport, create the fixed-deadline `pending` reconnect record, and begin durable speaking cleanup. |
| Durable speaking clear fails during transport-loss handling | Keep `pending`, preserve capacity and the original deadline, publish no reconnect events, and retry with bounded contexts/delays. |
| Reconnect is attempted from `pending`, `timing_out`, or at/after its deadline | Do not register or emit `member.restored`; close safely through the existing error path. |
| Durable timeout disconnect fails transiently | Keep `timing_out`, do not restore or free capacity, and retry without a fixed attempt limit. |
| HTTP leave and timeout race for the same member | Exactly the operation receiving `Transitioned=true` emits its terminal event; `Transitioned=false` emits none. |
| Burst `room.resync_requested` from one connection | Bound snapshot work and return retryable `room.error` with `rate_limited` or equivalent deterministic guard. |
| A required multi-envelope transition is queued with `ConnectionQueueSize=1` | Enqueue the envelopes as one ordered logical group; do not classify the healthy client as slow merely because the group has multiple events. |
| The finite logical-group queue is already full | Close/remove the slow connection safely so one client cannot block room fan-out. |

### 5. Good/Base/Bad Cases

- Good: WebSocket handshake verifies the query token, checks path-room match, authorizes persisted product room/member state, upgrades, then performs fresh registration checks and snapshot-first registration under the pinned room transition boundary.
- Good: mute/speaking repository methods load eligibility and write in one immediate transaction, repair `muted => !speaking`, and return the committed member/change flags consumed by the Hub.
- Good: a command pins its room, verifies exact connection ownership, calls the state mutator without `Hub.mu`, re-checks ownership, then allocates shared sequence values and queues the committed events in one short mutex section.
- Good: unexpected transport loss fixes the deadline at loss detection, durably clears speaking before entering `restorable`, retries transient failures without dropping the record, and enters non-restorable `timing_out` at the original deadline.
- Good: HTTP leave and reconnect timeout both honor `Transitioned`, so one durable transition produces at most one terminal room event.
- Base: snapshots are projections rebuilt from SQLite room/member state plus in-memory reconnect overlays and the receiving member ID; they are not persisted as separate state and do not advance room-wide shared sequence by themselves.
- Base: room gates, reference pins, sequence counters, connections, heartbeat pings, reconnect phases/timers, speaking throttle guards, resync guards, and logical-group send queues are in-memory single-instance transport state; empty room state is pruned.
- Base: queue grouping changes only internal delivery granularity. Every shared envelope retains its own public event type, payload, and sequence increment.
- Bad: accepting any `member_id` from a WebSocket client payload as authorization or deriving membership from LiveKit participants.
- Bad: authorizing/reading a member, later issuing an unconstrained update, and broadcasting a caller-reconstructed member; a concurrent leave, mute, or timeout can make all three stale.
- Bad: holding `Hub.mu` while waiting for a room gate, calling SQLite/snapshot/authorization code, scheduling timers, or performing a WebSocket close handshake; unrelated rooms then block behind external work.
- Bad: registering a connection before its snapshot is queued, failing to pin room state through the transition, or trusting pre-upgrade authorization for reconnect restoration.
- Bad: broadcasting reconnecting before speaking cleanup commits, resetting the deadline on retry, dropping a pending record after N failures, or restoring from `pending`/`timing_out`.
- Bad: enqueuing each envelope of one required transition separately so a capacity-one queue rejects the second envelope before the writer can drain the first.
- Bad: advancing speaking throttle time for a server-forced false, retaining throttle data after permanent departure, logging raw `?token=...` URLs, allowing unlimited resync work, or expanding this scope into new protocol events, chat, or cross-instance fan-out.

### 6. Tests Required

- Store/service transition tests:
  - mute and speaking eligibility plus write run under the same immediate transaction; a barrier-controlled concurrent leave/mute cannot be overwritten by a stale command;
  - inactive/disconnected members cannot commit mute/speaking changes, and the service maps stable room/member errors without reconstructing state;
  - committed transition results contain the persisted member and exact change flags;
  - muting clears speaking atomically, muted members cannot commit `speaking=true`, and both requested speaking values repair a historical `muted=true/speaking=true` row;
  - repeated leave/disconnect returns `Transitioned=false` after the first durable active-to-disconnected transition.
- WebSocket integration tests:
  - valid token cross-origin connection succeeds only when the Origin is explicitly allowed and receives immediate `room.snapshot`;
  - a connection cannot miss a room event between snapshot position selection and registration;
  - private snapshots, heartbeat pings, and room errors do not advance shared sequence for other clients;
  - missing, malformed, tampered, unsupported-version, expired, wrong-room, missing-room, expired-room, missing-member, and disconnected-member handshakes produce the documented status/code/message;
  - snapshot members retain joined-time/ID ordering, exclude disconnected members, and set exactly one `is_self`;
  - at least two already-connected clients receive `member.joined` after a later HTTP join;
  - mute/speaking commands broadcast only committed changes, persist into later snapshots, and do not reorder members;
  - a stale connection cannot start a mutation, and a barrier-controlled in-flight command cannot publish after detach/replacement;
  - blocking a state/snapshot dependency for room A does not prevent room B from completing, while same-room snapshot positions and shared events remain ordered;
  - HTTP leave broadcasts `member.left`, closes/removes the leaving connection, and removes it from later snapshots only for `Transitioned=true`;
  - heartbeat ping/pong keeps the connection usable;
  - unexpected disconnect fixes its deadline at loss time, durably clears speaking before the ordered reconnect group, preserves member identity/order/capacity, and projects `reconnecting` with `reconnect_until`;
  - reconnect expiration between handshake authorization and registration cannot restore or emit `member.restored`;
  - a valid restore receives `room.snapshot` first, then exactly one `member.restored` built from fresh data with recipient-specific `is_self`;
  - transient speaking-clear and timeout-disconnect failures retain `pending`/`timing_out`, use bounded contexts, retry beyond any fixed attempt count, publish only after commit, preserve the original deadline, and never permit late restoration;
  - a controlled HTTP leave/timeout race emits exactly one of `member.left` or `member.disconnected` according to `Transitioned` and frees capacity once;
  - `ConnectionQueueSize=1` delivers required multi-event transitions as ordered groups with one sequence increment per shared envelope, while a genuinely full group queue still removes a slow consumer;
  - a controlled clock proves server-forced `speaking=false` does not throttle the immediate next valid client `speaking=true` report;
  - burst resync requests are bounded while one normal resync succeeds;
  - permanent leave/disconnect removes throttle/reconnect data, failed initial snapshots do not retain empty room state, and no-client HTTP notifications do not retain rooms;
  - unknown and invalid client messages return `room.error` or safe close without mutation.
- HTTP/router tests:
  - WebSocket route is registered only when `WithRoomWebSocket` is configured;
  - redacted request preserves non-sensitive query fields, leaves the original request unchanged, redacts both `URL.RawQuery` and `RequestURI`, and still passes the original token to the hub;
  - authenticated HTTP leave cannot close/broadcast leave for another member when the bearer token member differs from the body `member_id`;
  - the leave notifier runs only for `Transitioned=true`.
- Store snapshot tests:
  - `ListRoomMembersByStates` returns only requested states, orders by `joined_at ASC`, then `id ASC`, and returns empty for empty state filters.
- Runtime wiring tests:
  - env-backed router can create a room and open the WebSocket snapshot path.
- Use channel/barrier-controlled fakes and an injected clock/scheduler for interleavings; sleeps must not be the primary correctness mechanism.
- Full backend check:

```bash
cd services/api
go test -count=20 ./internal/ws/...
go test -count=1 ./internal/room/... ./internal/store/...
go test -count=1 ./...
go vet ./...
cd ../..
git diff --check
```

Attempt `cd services/api && go test -race ./...` when the local Go/CGO toolchain supports it; record the exact toolchain limitation rather than weakening deterministic concurrency coverage.

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

Why correct: the bearer credential proves server-issued room/member claims, and the room service verifies current persisted product state before any WebSocket payload is trusted. Reconnect registration must still refresh this authorization inside its room transition boundary.

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

#### Wrong

```go
authorized, _ := h.authorizer.AuthorizeMemberContext(ctx, input)
h.mu.Lock()
result, _ := h.stateMutator.UpdateMemberSpeakingContext(ctx, command)
authorized.Member.Speaking = command.Speaking
h.broadcastLocked(authorized.Member)
h.mu.Unlock()
```

Why wrong: authorization and write are separate observations, the broadcast uses stale caller-derived state, external I/O runs under the global mutex, and the connection may no longer own the member slot.

#### Correct

```go
roomState, release := h.acquireRoomState(c.roomID, false)
defer release()

h.mu.Lock()
current := connectionIsCurrentLocked(roomState, c)
h.mu.Unlock()
if !current {
    return
}

result, err := h.stateMutator.UpdateMemberSpeakingContext(c.ctx, command)
if err != nil || !result.Changed {
    return
}

h.mu.Lock()
if connectionIsCurrentLocked(roomState, c) {
    // Allocate the shared sequence and enqueue result.Member in this short section.
}
h.mu.Unlock()
```

Why correct: the room pin and transition gate linearize same-room lifecycle work, the repository owns one immediate eligibility/write transaction, the Hub uses the committed member, and `Hub.mu` never covers the external mutation call.

#### Wrong

```go
client.send <- newOutboundGroup(snapshot)
client.send <- newOutboundGroup(restored)
```

Why wrong: a valid capacity-one queue can accept the first envelope and reject the second before the writer drains it, incorrectly classifying a healthy client as slow.

#### Correct

```go
client.send <- newOutboundGroup(snapshot, restored)
```

Why correct: queue capacity counts one ordered logical transition, while `snapshot` and `restored` retain their own envelope semantics and shared events retain their own sequence increments.

#### Wrong

```go
h.broadcastReconnectAvailable(roomState, memberID, generation, deadline, true, h.currentTime())
_, err := h.stateMutator.UpdateMemberSpeakingContext(ctx, clearSpeaking)
deadline = h.currentTime().Add(h.reconnectWindow)
```

Why wrong: clients observe reconnecting before the durable clear, and moving the deadline gives retries a new restore window.

#### Correct

```go
deadline := lostAt.Add(h.reconnectWindow)
mutationContext, cancel := context.WithTimeout(context.Background(), reconnectMutationTimeout)
result, err := h.stateMutator.UpdateMemberSpeakingContext(mutationContext, room.UpdateMemberSpeakingInput{
    RoomID: roomID, MemberID: memberID, Speaking: false,
})
cancel()
if err != nil {
    h.scheduleReconnect(roomState, roomID, memberID, generation, reconnectRetryDelayUntil(deadline, h.currentTime()))
    return
}
h.broadcastReconnectAvailable(roomState, memberID, generation, deadline, result.Changed, h.currentTime())
```

Why correct: publication follows the committed speaking clear, retries retain one loss-time deadline, each attempt is bounded, and attempt count remains unbounded until a durable or stable terminal outcome.
