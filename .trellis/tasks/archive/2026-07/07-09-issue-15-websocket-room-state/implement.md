# WebSocket room snapshot and member broadcasts — Implementation Plan

## Preconditions

- Branch from latest `master` before implementation: `issue-15-websocket-room-state`.
- Task remains in `planning` until this PRD/design/implement set is reviewed and `task.py start` is run.
- Do not implement excluded features: mute, speaking, reconnect state transitions, chat, cross-instance fan-out, frontend UI.

## Ordered checklist

### 1. Prepare backend dependency and package shell

- Add `github.com/coder/websocket` and `github.com/coder/websocket/wsjson` to `services/api/go.mod` if not already present.
- Create `services/api/internal/ws/` with small files as needed:
  - hub/connection lifecycle;
  - message envelope and payload structs;
  - snapshot projection helpers;
  - auth/error helpers.
- Keep public constructors small and dependency-injected for tests.

### 2. Define hub contracts and interfaces

- Define interfaces over existing services/repositories instead of importing HTTP internals:
  - room member authorizer compatible with `AuthorizeMemberContext`;
  - room leaver if WebSocket leave command is implemented for `member.leave_requested` subset;
  - snapshot repository for `FindRoomByID`, `FindMemberByRoomAndID`, and active member listing.
- Add a repository method for active member listing ordered by join time if needed.
- Use existing `domain.Room`, `domain.Member`, and `session.Verify` rather than duplicate token or product-state logic.

### 3. Write requirement-driven tests first

Create or update tests before implementation:

- `services/api/internal/ws/hub_test.go`
  - valid token connection receives immediate `room.snapshot`;
  - missing/malformed/tampered/expired/wrong-room token rejection;
  - missing room, expired room, missing member, disconnected member rejection;
  - snapshot ordering and `is_self` personalization;
  - multi-client HTTP join -> `member.joined` broadcast;
  - multi-client HTTP leave -> `member.left` broadcast and later snapshot exclusion;
  - heartbeat ping/pong happy path;
  - heartbeat timeout removes/closes connection;
  - unknown/invalid message returns `room.error` or safe close without mutation.
- `services/api/internal/http/router_test.go` or a WebSocket-specific HTTP integration test
  - route registration is conditional on hub option;
  - existing routes still work.
- `services/api/internal/store/*_test.go` if adding member listing repository methods.
- `services/api/cmd/api/main_test.go` if startup wiring changes enough to need router seam coverage.

### 4. Implement snapshot projection

- Convert `domain.Room` to WebSocket room object using contract field names (`room_id`, not HTTP `id`).
- Convert `domain.Member` to member object:
  - `member_id` from `Member.ID`;
  - `is_self` based on receiver member ID;
  - `is_host`, `muted`, `speaking`, `voice_mode`, `joined_at` from domain;
  - `state` only `online`/`reconnecting` for snapshots;
  - `reconnect_until` null for Issue #15 because reconnect transitions are out of scope.
- Order members by `joined_at` and stable ID tie-breaker.
- Ensure disconnected members are excluded.

### 5. Implement pre-upgrade auth and errors

- Extract `token` query parameter; reject blank token with `401 invalid_room_session`.
- Verify token with configured `RoomSessionSecret` and injectable `Now`.
- Compare claims room ID to path room ID before product lookup.
- Call existing `AuthorizeMemberContext` to validate room/member state.
- Map errors exactly to WebSocket contract pre-upgrade matrix.
- Avoid logging raw query strings or token values.

### 6. Implement hub connection and sequencing

- Maintain in-memory map by room ID and member ID.
- A valid new connection for the same room/member may replace a stale prior connection.
- Generate per-room monotonically increasing `seq` for `room.snapshot`, `member.joined`, `member.left`, and any `room.error`/heartbeat choice that uses sequenced envelopes.
- Use write deadlines or per-connection send queues to avoid one slow client blocking room fan-out.
- On connection cleanup, remove only transport state for Issue #15; do not mark member reconnecting/disconnected unless explicit leave was requested.

### 7. Implement join/leave hub notifications

- Extend HTTP handler/router wiring so successful `JoinRoom` notifies the hub with returned room/member.
- Extend successful `LeaveRoom` to notify the hub with returned room/member and server time.
- Broadcast `member.joined` to already-connected clients in the same room.
- Broadcast `member.left` to remaining clients and close/remove the leaving member connection if present.
- Keep HTTP response semantics unchanged (`join` returns 200 body; `leave` returns 204 empty body).

### 8. Implement basic heartbeat and client command handling

- Send JSON `heartbeat.ping` every 15 seconds by default; tests should use short injectable intervals.
- Track outstanding ping IDs and accepted `heartbeat.pong` messages.
- Close/remove connection after timeout when no valid pong arrives.
- Handle `room.resync_requested` by sending a fresh `room.snapshot` if low-cost to implement; otherwise return `room.error` only if this remains outside Issue #15. Prefer implementing snapshot response because it reuses projection and is in the existing contract.
- Handle `member.leave_requested` only if it can reuse the existing leave service without expanding scope. If implemented, broadcast `member.left` then close normal. If not implemented, document/test as unsupported with no mutation. HTTP leave is required regardless.
- Unknown/out-of-scope messages must not mutate product state.

### 9. Wire runtime startup

- In `cmd/api/main.go`, create one hub using the same repository, room service, credential config, and defaults.
- Pass the hub into `httpapi.NewRouter` through a new option.
- Preserve `FromEnv()` use for runtime config.

### 10. Documentation and contract alignment

- Update `docs/api/websocket.md` only if necessary to mark the Issue #15 implemented subset or clarify basic heartbeat behavior.
- Do not remove broader MVP contract entries that future issues will implement.
- If no contract wording changes are needed, leave the doc untouched.

## Validation commands

Run from repository root unless stated otherwise:

```bash
go test -count=1 ./services/api/...
git diff --check
```

If dependency/workspace files change, also run:

```bash
go work sync
```

If WebSocket tests use short heartbeat intervals, run the focused package during iteration:

```bash
go test -count=1 ./services/api/internal/ws -v
```

## Review gates

- Every PRD acceptance criterion has at least one test or explicit manual rationale.
- Error status/code/message values match `docs/api/websocket.md`.
- Existing HTTP tests still pass without changed expectations.
- No logs or committed examples contain plaintext tokens/secrets.
- No frontend, chat, mute/speaking/reconnect, Redis, or cross-instance work slipped into the diff.

## Rollback points

- If the `ws` package design becomes too broad, revert to only endpoint auth + snapshot + join/leave broadcast before adding heartbeat extras.
- If `coder/websocket` introduces unmanageable test/toolchain issues, stop and document the incompatibility before choosing another library; do not silently replace the project-planned dependency.
- If implementing `member.leave_requested` risks changing room lifecycle semantics, keep leave broadcasts driven by HTTP leave only and leave WebSocket leave command for a later task.
