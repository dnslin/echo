# Fix WebSocket Room-State Review Findings

## Goal

Repair the WebSocket room-state consistency defects identified in PR #38 while preserving the existing public protocol and MVP boundaries. The API service must remain the authority for temporary-room member lifecycle, mute state, and speaking state.

## Requirements

1. A member mute or speaking command must change durable state only when the same member is still eligible to make that transition. Concurrent HTTP leave, reconnect timeout, mute, and speaking commands must not leave `muted=true/speaking=true`, `disconnected/speaking=true`, or emit an event for a transition that did not commit.
2. A WebSocket command accepted from a connection must be applied and broadcast only while that exact connection is the current registered connection for its room/member. A connection that is being replaced, disconnected, or moved into reconnecting must not later publish a stale state transition.
3. Reconnect registration must check the current durable member state and the in-memory reconnect deadline at the registration linearization point. An expired or already-disconnected member must not regain a room slot or receive `member.restored`.
4. An accepted reconnect must preserve the original member ID and list position, send `room.snapshot` as its first visible event, then broadcast exactly one `member.restored`. Each recipient's `is_self` projection must be correct, and restored state must reflect the current authoritative member data.
5. Unexpected disconnect must durably clear speaking before authoritative reconnecting state is published. If persistence is temporarily unavailable, preserve a pending/timing-out state, retry with bounded delay and no attempt-count limit, and never allow restoration after the original disconnect deadline has elapsed.
6. HTTP leave and reconnect timeout must have one durable terminal winner. Observers receive at most one terminal room event (`member.left` or `member.disconnected`) for that member transition, and capacity is released only after durable disconnect succeeds.
7. A valid low queue configuration must not close a healthy connection merely because one logical state transition contains multiple ordered events. Backpressure remains bounded for genuinely slow consumers.
8. All transitions in one room that select a snapshot position or change shared room state—registration, snapshots, joins, leaves, commands, reconnects, and timeouts—must be linearized by one per-room boundary. SQLite/snapshot I/O for that boundary must not hold the Hub-wide mutex or block unrelated rooms. Potentially blocking WebSocket close handshakes must not delay lifecycle handoff or reconnect deadline creation.
9. Only client-accepted speaking reports may advance the speaking-throttle timestamp. Server-forced `speaking=false` caused by mute, reconnect, leave, or disconnect must not suppress the next valid client `speaking=true` report.
10. Per-member transient Hub data, including speaking-throttle timestamps, must be removed after a permanent leave/disconnect. Failed initial snapshot registration must not retain an otherwise empty room state.

## Constraints

- Keep the public endpoint, JSON envelope types, sequence semantics, and Chinese API error copy compatible with `docs/api/websocket.md`.
- Do not add accounts, cross-instance fan-out, Redis, new room-management actions, LiveKit-derived membership, or persistent reconnect timers.
- SQLite remains the durable authority for member lifecycle, mute, and speaking state; Hub maps/timers remain single-instance transport state.
- The fix must preserve `room.snapshot` as the first client-visible message and must not reorder members on speaking transitions.
- Use deterministic automated backend tests; desktop/media manual acceptance is out of scope for this server-side state repair.

## Acceptance Criteria

- [ ] A deterministic service/store test proves that a stale mute or speaking operation cannot overwrite a concurrent leave, mute, or reconnect cleanup transition.
- [ ] A deterministic Hub test proves that an in-flight command from an old connection cannot broadcast after that connection has entered reconnecting or been replaced.
- [ ] A reconnect test proves that a deadline expiring between handshake authorization and registration rejects restoration and does not emit a stale `member.restored`.
- [ ] A restored connection receives `room.snapshot` first, then observes `member.restored` with exactly one correct recipient-specific `is_self` projection and current `speaking=false` state.
- [ ] A temporary durable failure in reconnect cleanup retains a pending/timing-out record, retries after bounded delays without an attempt-count cutoff, later succeeds without ghost capacity, and never publishes a durable transition before it has committed.
- [ ] Once the original reconnect deadline has elapsed, a pending retry cannot restore the member even if durable cleanup has not yet succeeded.
- [ ] A controlled HTTP leave/reconnect-timeout race produces exactly one terminal room event and releases capacity exactly once.
- [ ] `ConnectionQueueSize=1` handles each required multi-event transition as an ordered logical group without closing a healthy client.
- [ ] A blocked state operation for room A does not prevent room B from completing its independent state transition, while same-room snapshot positions and shared events remain linearized.
- [ ] With a controlled clock, a server-forced `speaking=false` from mute or reconnect does not throttle the member's immediate next valid client `speaking=true` transition.
- [ ] Permanent leave/disconnect removes the member's throttle entry; a snapshot-build error leaves no empty Hub room state.
- [ ] `go test -count=1 ./...`, focused repeated WebSocket tests, `go vet ./...`, and `git diff --check` pass from `services/api`.

## Non-Goals

- Replacing the single-instance Hub with distributed coordination.
- Changing client voice detection, media transport, token issuance, or desktop UI behavior.
- Introducing replay history, a new command protocol, or user-visible retries beyond the current reconnect semantics.
