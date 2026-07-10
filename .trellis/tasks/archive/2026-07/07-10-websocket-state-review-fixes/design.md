# WebSocket Room-State Consistency Repair Design

## Context

PR #38 added durable mute/speaking state, in-memory reconnect windows, reconnect timeout handling, and shared WebSocket state broadcasts. Review found that the implementation has multiple independently reachable interleavings where durable state, Hub overlays, connection ownership, and broadcasts disagree.

The repair deliberately treats this as a state-transition ownership problem rather than a collection of handler-local guards.

## First-Principles Analysis

### Assumptions to reject

1. A successful authorization read authorizes a later write. It does not: another transaction can change the member between the read and write.
2. A connection that started a handler remains the member's current connection when the handler publishes. It does not: close/reconnect can replace it while its storage call is in flight.
3. An in-memory reconnect entry always represents an unexpired, durable-active member. It does not: timers can run late and durable transition calls can fail.
4. A queue capacity of one can carry any valid transition. It cannot carry two separately enqueued messages before the writer consumes one.
5. A Hub-wide mutex is harmless because the server has few members. A blocking SQLite call or close handshake under that mutex blocks every room, regardless of room size.

### Bedrock invariants

- SQLite member rows are the durable source of truth for `state`, `muted`, and `speaking`.
- A broadcast is authoritative to clients only if it represents a committed durable transition or a defined in-memory projection that is serialized with its lifecycle owner.
- A connection-specific command is valid only while its connection owns the `(roomID, memberID)` registration slot.
- A reconnect deadline is a hard time boundary; a delayed timer cannot make a late restore valid.
- A single logical state transition may require multiple ordered client messages, but it must not be self-classified as backpressure before the writer has a chance to process the group.

## Design Decisions

### 1. Make durable member transitions atomic at the repository boundary

Replace the current `AuthorizeMemberContext` followed by unconstrained `UPDATE` sequence for mute/speaking with repository-owned transition operations that run in the same SQLite immediate transaction style as join/leave.

Each operation must:

1. load the room/member inside the transaction;
2. verify the room is active and the member is still `online` for established-connection commands;
3. apply the transition only when the requested state differs;
4. enforce `muted => speaking=false` and reject/ignore `speaking=true` when muted;
5. return the committed member plus explicit change/terminal information.

The room service remains responsible for public error mapping and result shapes. It must no longer derive an authoritative updated member from a stale pre-update read.

The leave/disconnect repository transition must also return whether it actually moved an active member to `disconnected`. An idempotent observation of an already-disconnected member is not a new terminal transition.

### 2. Linearize all transitions for one room without holding the Hub-wide mutex during I/O

Add one per-room transition gate owned by the Hub. It is deliberately broader than a per-member lock because the following operations must share one linear order to preserve snapshot-first behavior and shared `seq` semantics:

- initial registration/restoration and private resync snapshots that select a snapshot sequence position;
- HTTP join/leave notification and connection removal;
- mute/speaking commands;
- unexpected transport loss;
- reconnect timeout and retry;
- shared event allocation and delivery grouping for that room.

Within that gate, a command checks that its connection is still `byMember[memberID]` before durable mutation and again before publishing. Acquire/release or reference-count the per-room state for the full operation lifetime, so pruning cannot remove a room while an in-flight transition later writes to an orphaned state object.

`Hub.mu` remains only for short room-map, connection-map, sequence, and queue mutations. The per-room gate—not `Hub.mu`—may cover room service, SQLite, or snapshot-store I/O, so a blocked room A transition cannot block room B at the Hub layer. SQLite itself has a single-writer constraint; this design does not promise concurrent durable writes. Potentially blocking `websocket.Conn.Close` is outside both the global mutex and the lifecycle handoff critical path.

Define and test one lock order: acquire the per-room transition gate before the brief `Hub.mu` section, and never invoke external dependencies while `Hub.mu` is held. This prevents deadlocks among snapshot registration, timeout, HTTP leave, restoration, and command paths.

### 3. Reconnect lifecycle is durable-first, deadline-aware, and non-restorable while pending

On unexpected transport loss:

1. cancel and detach the connection from Hub ownership before any potentially blocking close handshake;
2. record a serialized reconnect record whose original deadline is measured at loss detection and whose phase is `pending` until durable speaking clear succeeds;
3. durably set `speaking=false` before publishing `member.speaking_changed(false)` or `member.reconnecting`;
4. after success, publish the ordered reconnect transition and schedule timeout against the original absolute deadline.

For a temporary persistence error, retain the record and retry after a bounded delay. Delay/backoff is bounded; retry attempts are not. Never delete the record merely because a retry budget was exhausted. Once the original deadline has elapsed, the record enters `timing_out`; it cannot be restored even while durable disconnect is still retrying. A stable terminal result caused by concurrent HTTP leave removes the overlay without emitting `member.disconnected`.

At restoration, acquire the room gate, re-authorize current durable state, inspect the current reconnect record and its absolute deadline, and reject a `pending` or `timing_out` record as well as a deadline at or before `now`. A late timer can never turn a late restore into a valid one.

The restored payload must use freshly read/projected member data. The same shared sequence can be used for all recipients, but `member.restored.payload.member` must be projected separately per receiving connection so only the restored connection gets `is_self=true`.

### 4. Give terminal events a single durable winner

HTTP leave and reconnect timeout both rely on the repository transition result:

- If HTTP leave committed the active-to-disconnected transition, it removes reconnect overlay and emits `member.left` once.
- If reconnect timeout committed it, it removes overlay and emits `member.disconnected` once.
- The loser observes `Transitioned=false`, removes any obsolete local state, and emits no terminal event.

This keeps HTTP command semantics idempotent while ensuring observers never see both event types for one member departure.

### 5. Queue logical event groups atomically

The send queue must represent an ordered outbound group rather than a single envelope. Examples of one group are:

- `room.snapshot`, then `member.restored` for a restored connection;
- `member.speaking_changed(false)`, then `member.reconnecting`;
- `member.speaking_changed(false)`, then `member.muted_changed`.

The writer serializes envelopes within a group before taking the next group. A queue capacity of one can therefore accommodate a valid transition group, while a genuinely blocked writer can still fill the finite group queue and be closed as a slow consumer. Shared sequence allocation remains one increment per shared event; grouping changes delivery atomicity, not event numbering.

### 6. Separate client-report throttling from server-enforced clears

`lastSpeakingAccepted` is a client-report throttle, not a general member-state timestamp. Only an accepted client-originated speaking transition may update it. A server-forced false caused by mute, reconnect, leave, or disconnect must delete or leave unchanged the throttle entry; it must not make the next valid client `speaking=true` report appear too frequent.

Use the injected `Now` function to test a forced false at `T` followed by a client true at `T + 1ms`. The latter is a new desired state and must reach the durable mutator when other eligibility checks pass.

### 7. Cleanup and error paths are part of the state machine

- Remove `lastSpeakingAccepted[memberID]` after a permanent HTTP leave or successful/stable reconnect terminal outcome.
- Prune a room state after initial snapshot construction fails if it has neither connections nor reconnecting/pending entries.
- Stop/release reconnect timers only after the serialized transition confirms they are obsolete.
- Move lifecycle handoff ahead of the transport close handshake so a peer that does not complete close cannot delay reconnect state. The close path must still be once-only and must safely stop writer/read/heartbeat goroutines.

## Compatibility

No public endpoint, JSON field, or event type changes are required. The externally visible correction is stricter consistency:

- stale commands yield the existing `member_not_active` / recoverable error behavior instead of an invalid broadcast;
- a late reconnect is rejected rather than restored;
- clients see one terminal event and correct per-recipient `is_self` values;
- retry under a transient SQLite failure retains reconnecting/capacity rather than silently losing the member's lifecycle record.

## Test Strategy and Feedback Loop

Use the existing `ws` in-package integration fixture (temporary SQLite, controlled `Config`, test WebSocket clients) plus purpose-built blocking authorizer/state-mutator/store fakes and a manual clock/scheduler seam. Tests must control barriers/channels rather than rely on `time.Sleep` races. For the cross-room non-blocking assertion, block before entering the real SQLite writer (or block a snapshot read): SQLite intentionally permits one writer, so two real `BEGIN IMMEDIATE` writes are not a valid parallelism expectation.

| Root cause | Deterministic signal before fix | Expected signal after fix |
| --- | --- | --- |
| stale durable mutation | release a command after a controlled leave/mute transition; assert invalid row/event | mutation is rejected/no-op and no stale event is sent |
| stale connection handler | block command mutator, detach/replace connection, then release it | old handler cannot publish after reconnecting/replacement |
| reconnect TOCTOU | expire deadline or durable member at registration barrier | no snapshot/restored registration for invalid restore |
| retry/terminal race | fail disconnect N times or race HTTP leave with timeout through barriers | exactly one terminal winner; retry state remains until a durable outcome |
| queue grouping | `ConnectionQueueSize=1` and force a two-event transition before writer drains | ordered events arrive; no healthy connection close |
| room linearization | race snapshot registration/resync with a same-room shared event | snapshot-first and shared `seq` ordering remain coherent |
| Hub lock scope | block room A mutator and progress independent room B | room B completes before room A is released |
| forced speaking clear throttle | force false at controlled time `T`, report true at `T + 1ms` | true reaches the mutator and broadcasts when eligible |

## Risks and Mitigations

- **Lock-order/pruning regressions:** document and test the per-room-gate/Hub-lock ordering; keep a transition gate reachable while its operation is in flight, and do not call external dependencies under `Hub.mu`.
- **Expanded repository result contract:** update all service fakes and tests in the same change; keep public HTTP/WebSocket response contracts unchanged.
- **Retry goroutine lifetime:** retain a timer only while a pending durable transition exists, stop it on all terminal paths, and assert Hub pruning.
- **Event ordering drift:** tests must assert snapshot-first, per-event sequence increments, and ordered grouped delivery.
- **SQLite contention:** use context deadlines for retried work; validate that temporary failure is retried but a stable terminal state is not rebroadcast.

## Rollback Shape

The change is internal to the API service and schema-free. If a regression appears before release, revert the code/test commit as one unit. No data migration or protocol compatibility rollback is required.
