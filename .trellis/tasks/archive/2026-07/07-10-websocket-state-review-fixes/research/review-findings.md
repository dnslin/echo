# PR #38 Review Findings Research

## Sources Reviewed

- `services/api/internal/ws/hub.go`
- `services/api/internal/ws/hub_state_test.go`
- `services/api/internal/ws/hub_test.go`
- `services/api/internal/room/service.go`
- `services/api/internal/store/sqlite.go`
- `services/api/internal/http/handlers.go`
- `docs/api/websocket.md`
- `.trellis/spec/backend/websocket-room-state-guidelines.md`
- ADRs 0015, 0016, 0019, 0020, and 0029

## Product Facts

1. The API service owns product temporary-room membership and state; LiveKit owns media transport only.
2. SQLite durably owns member lifecycle, `muted`, and `speaking`. Reconnect deadline/timer/connection/sequence state are intentionally Hub-local for the single-instance MVP.
3. Reconnecting members retain member ID, ordering, and capacity for 30 seconds, must be projected as `speaking=false`, and must be durably disconnected only after timeout.
4. `room.snapshot` must be first for an accepted connection. Shared broadcasts increment `seq`; private snapshots/errors/pings do not.
5. Member object projection is recipient-specific: only the receiving member has `is_self=true`.
6. A valid client `speaking=true` transition must not be discarded merely because the server itself previously forced the member's speaking state to false.

## Confirmed Review Findings Grouped by Root Cause

### A. Durable member state transitions are not atomic

`room.Service.UpdateMemberMuteContext` and `UpdateMemberSpeakingContext` authorize/read first, then `store.Repository` writes using only `room_id` and `id`. A concurrent leave, disconnect, or mute can occur between those operations. Consequences include a muted or disconnected row being written back to `speaking=true`, and a Hub broadcast based on stale service results.

Required repair: repository-owned transaction with eligibility predicates and committed result; no caller-derived post-write member projection.

### B. Connection/lifecycle transition ownership and room ordering are missing

A command handler can finish its state-mutator call after the connection is removed and reconnecting has already published `speaking=false`. It then obtains `Hub.mu` and emits stale `speaking=true`. Registration uses pre-upgrade `authorizedMember` and only tests reconnect-map presence, allowing stale projection/deadline issues.

The serialization boundary must be **per room**, not merely per member: snapshot-first registration and shared `seq` ordering must linearize with joins, leaves, commands, reconnect events, timeout events, and resync snapshots for that room. The global Hub mutex should only protect the room map and short map/queue mutations; SQLite/snapshot I/O runs under the per-room transition gate instead, preventing cross-room head-of-line blocking.

Required repair: per-room lifecycle serialization, current connection ownership checks, fresh authorization at registration, deadline validation, and fresh recipient-specific projections.

### C. Reconnect persistence and terminal handling are not reliable

Unexpected disconnect broadcasts false/reconnecting even when the durable clear-speaking mutation failed. Reconnect timeout retries only once and then deletes the local overlay on the second error, leaving durable `online` capacity stuck. Timeout and HTTP leave can both return successful idempotent transitions and emit different terminal events.

Required repair: durable-first reconnect publication, pending/timing-out state that prevents restoration after the original loss deadline, retry with bounded delay but no attempt-count cutoff, and a durable `Transitioned` result that elects one terminal broadcaster. The deadline is always measured from transport-loss detection, never from a retry.

### D. Delivery and Hub resource boundaries are too coarse

A valid queue size of one cannot hold snapshot plus restored or other fixed event pairs before the writer drains, so a healthy client can be closed as slow. SQLite writes occur under global `Hub.mu`; `closeWithMode` performs transport close before lifecycle handoff. Failed snapshot room states can persist unnecessarily.

Required repair: queue logical event groups, no external I/O under global Hub mutex, lifecycle handoff before potentially blocking close, and explicit cleanup on permanent/error paths.

### E. Server-forced speaking clears incorrectly consume client-report throttle budget

`hub.go` writes `lastSpeakingAccepted` when a mute or unexpected disconnect forces `speaking=false` (currently at the mute and disconnect broadcast paths). The next valid client `speaking=true` within `SpeakingMinInterval` is dropped before reaching the durable mutator. This is visible after a quick unmute or a successful restore: a transition-reporting client may not send another edge, leaving its indicator false.

Required repair: update `lastSpeakingAccepted` only for accepted client-reported speaking transitions. Server-enforced false transitions must delete or otherwise not advance the throttle entry. Verify this with a controlled `Now` function, not a sleep.

## Deterministic Feedback Loop Design

The existing in-package WebSocket tests already provide temporary SQLite, real router/Hub wiring, fixed time injection, and direct access to unexported Hub state. Extend that seam with channel-controlled fakes:

- `blockingStateMutator`: signals when a command has reached the mutation boundary and resumes only when the test releases it.
- `dynamicAuthorizer` / `blockingSnapshotStore`: changes durable authorization or deadline state between handshake and registration.
- `flakyDisconnectMutator`: fails a known number of disconnect calls, then succeeds.
- `barrierLeaveNotifier` or transition result fake: creates a deterministic timeout/HTTP leave ordering.
- direct outbound group inspection or a controlled writer: verifies capacity-one delivery without scheduler-dependent sleeps.
- a controlled `Now` closure: advances exactly within the speaking throttle interval after server-enforced mute/reconnect clear.

The test should use channel receive/send with bounded test contexts. Retry tests may use a configurable internal delay or eventual assertion with a bounded deadline; production code must not use a fixed retry attempt limit, and must retain its original reconnect deadline through all retries. Cross-room progress tests must block before the real SQLite writer or block a snapshot read; SQLite's single-writer design means two real `BEGIN IMMEDIATE` writes are not a valid parallelism assertion.

## Initial Hypotheses and Falsifiable Predictions

1. If authorization/write TOCTOU is the cause, pausing after the eligibility read and committing leave/mute before release will expose an invalid persisted member or stale broadcast.
2. If stale connection ownership is the cause, pausing a speaking mutator, detaching/reconnecting the connection, then releasing it will emit `speaking=true` after the reconnect sequence.
3. If terminal ownership is missing, gating timeout and HTTP leave to both observe the member will produce both `member.disconnected` and `member.left`.
4. If queue granularity is the cause, capacity one plus an ordered two-event state transition will close a connection without client-side backpressure.
5. If global lock scope is the cause, blocking persistence for room A will prevent an independent room B operation from completing until room A is released.
6. If forced false transitions consume throttle state, after a controlled server-forced false at time `T`, a valid client true at `T + 1ms` is ignored despite being a new desired state.

## Review Scope Clarification

The design that stores mute/speaking in SQLite is intentional for this task. The repair must not move those fields to a purely in-memory Hub source of truth. The narrow synchronous-close concern is retained as an ordering correction: not every idle reader experiences the full close timeout after cancellation, but a handler-in-progress connection with a non-cooperating peer can delay lifecycle handoff under the current ordering.
