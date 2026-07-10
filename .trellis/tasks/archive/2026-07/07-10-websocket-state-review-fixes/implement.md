# Implementation Plan

## Scope

Repair the PR #38 WebSocket room-state findings on branch `issue-16-websocket-state-broadcast`, targeting `master`. This is one integrated task because the Hub and SQLite transition rules jointly define the same member lifecycle invariant.

## Preconditions

- Read task context in this order: manifests, `prd.md`, `design.md`, then this file.
- Read `.trellis/spec/backend/database-guidelines.md`, `.trellis/spec/backend/error-handling.md`, and `.trellis/spec/backend/websocket-room-state-guidelines.md` before editing.
- Preserve the public contract in `docs/api/websocket.md`; update it only if a clarified existing guarantee needs explicit wording.
- Start with failing regression tests. Do not use timing sleeps as the primary interleaving mechanism.

## Ordered Work

### 1. Establish a deterministic baseline and regression seams

- [ ] Run focused existing room/store/WebSocket tests and the full API suite to establish the pre-change baseline.
- [ ] Add channel/barrier-based test doubles in the relevant `_test.go` files for: a blocked pre-store mutation, dynamic authorization, transient disconnect failures, a manual clock/scheduler, and controllable queue/writer behavior.
- [ ] For cross-room progress tests, block a pre-store fake or snapshot read rather than requiring two real SQLite `BEGIN IMMEDIATE` write transactions to complete concurrently; SQLite is intentionally a single-writer store.
- [ ] Write each regression test so it fails against the current behavior before production code changes.
- [ ] Record the exact pre-fix failures in the task research notes or journal.

Primary files:

- `services/api/internal/ws/hub_state_test.go`
- `services/api/internal/ws/hub_test.go`
- `services/api/internal/room/service_test.go`
- `services/api/internal/store/sqlite_test.go`

### 2. Make member persistence transitions atomic

- [ ] Refactor `services/api/internal/store/sqlite.go` so mute and speaking mutation checks plus writes occur inside one immediate SQLite transaction and return the committed member/change result.
- [ ] Ensure established commands require the member to remain `online`, prevent `speaking=true` for a muted member, and always enforce `muted=true => speaking=false`.
- [ ] Extend the leave/disconnect transition result to distinguish an actual active-to-disconnected transition from an already-disconnected idempotent result.
- [ ] Refactor `services/api/internal/room/service.go` to delegate authoritative state and change flags to the repository transaction rather than reconstruct them from a pre-write authorization result.
- [ ] Update `services/api/internal/http/handlers.go` so HTTP leave preserves its current idempotent `204` response but calls `NotifyMemberLeft` only when `Transitioned=true`.
- [ ] Add service/store regression coverage for leave/mute/speaking interleavings and terminal-result idempotency.

### 3. Linearize Hub room state and remove blocking work from the global mutex

- [ ] Introduce a per-room transition gate with acquire/release or reference counting, document the one lock order, and ensure prune cannot remove state while an operation is in flight.
- [ ] Use it for registration, private snapshots/resync position selection, join/leave notification, command handling, unexpected disconnect, reconnect timeout/retry, and restoration so snapshot-first and shared `seq` remain linearized for the room.
- [ ] Check current connection ownership before durable mutation and again before broadcast; suppress stale command results from detached/replaced connections.
- [ ] Move state-mutator and snapshot-store calls out of `Hub.mu`; use bounded contexts for retryable background work.
- [ ] Move Hub lifecycle handoff ahead of a potentially blocking WebSocket close handshake while preserving once-only shutdown.
- [ ] Add a same-room snapshot/shared-event ordering test, a two-room blocked-mutator test, and an old-connection-handler test.

Primary files:

- `services/api/internal/ws/hub.go`
- `services/api/internal/ws/hub_test.go`
- `services/api/internal/ws/hub_state_test.go`

### 4. Correct reconnect registration, timeout, and terminal event ownership

- [ ] Reauthorize/refresh member data at the registration linearization point and reject restoration if durable state is no longer active, the reconnect record is pending/timing-out, or the original in-memory deadline has elapsed.
- [ ] Treat speaking-clear persistence as a prerequisite for reconnect publication; retain a pending record whose deadline is fixed at loss detection and retry transient failures without silently discarding capacity state.
- [ ] Continue retrying reconnect terminal persistence with bounded delay but no attempt-count cutoff; after the original deadline, prevent restoration until durable success or a stable competing terminal result.
- [ ] Use the durable `Transitioned` result to allow exactly one of HTTP leave and reconnect timeout to broadcast its terminal event.
- [ ] Build recipient-specific `member.restored` projections using fresh authoritative data, including correct `is_self`.
- [ ] Add tests for registration-after-expiry, pending-after-expiry non-restoration, stale restored state, retry-after-transient-failure, and HTTP leave/timeout arbitration.

### 5. Make ordered event delivery safe for valid small queues and clean transient state

- [ ] Represent a logical ordered state transition as one outbound queue group; retain one shared `seq` increment per shared event.
- [ ] Cover restore, mute-clears-speaking, and reconnect-clears-speaking event groups.
- [ ] Preserve bounded slow-consumer behavior after a genuine finite queue of groups fills.
- [ ] Advance `lastSpeakingAccepted` only for accepted client-originated speaking reports; clear or preserve—not advance—the entry on server-forced false transitions.
- [ ] Delete `lastSpeakingAccepted` when the member permanently leaves/disconnects.
- [ ] Prune empty room state when initial snapshot construction fails.
- [ ] Add queue-size-one, per-recipient `is_self`, controlled-clock forced-false throttle, permanent-cleanup, and snapshot-error pruning tests.

### 6. Verification and review gates

- [ ] Run targeted tests repeatedly: `go test -count=20 ./internal/ws/...`.
- [ ] Run relevant room/store tests: `go test -count=1 ./internal/room/... ./internal/store/...`.
- [ ] Run full backend tests: `go test -count=1 ./...`.
- [ ] Run `go vet ./...` and `git diff --check`.
- [ ] Attempt `go test -race ./...`; if the local Windows toolchain lacks CGO/GCC, record the exact limitation and keep deterministic concurrency tests as the verification signal.
- [ ] Have a `trellis-check` agent independently verify contract compliance, test coverage, error paths, lock scope, and no accidental protocol expansion.
- [ ] Update the relevant spec only if the fix establishes a reusable invariant that is currently undocumented.

## Required Regression Scenarios

1. A speaking command reads eligibility, a concurrent transition disconnects/mutes the member, then the command resumes: no invalid row and no stale broadcast.
2. A mute command is in flight when another lifecycle transition occurs: the committed member and emitted events match one atomic durable result.
3. A command from a connection that is detached/replaced while its mutator is blocked cannot publish after `member.reconnecting`/replacement.
4. A reconnect deadline expires between pre-upgrade authorization and registration: the socket does not receive a successful restore.
5. Restored clients receive snapshot first and a recipient-specific restored projection with exactly one `is_self=true`.
6. Reconnect speaking-clear and final disconnect failures retain the original deadline, retry without an inconsistent broadcast, lost timer, leaked capacity, or late restoration.
7. HTTP leave and reconnect timeout race: exactly one terminal event is observed and the durable member becomes disconnected once.
8. Queue size one safely delivers a logical multi-event group; genuine slow consumers remain bounded.
9. Same-room snapshot/resync and shared-event work preserve snapshot-first and shared `seq` linearization; a blocked room A mutation does not delay room B state processing.
10. A server-forced false at controlled time `T` does not throttle the next valid client true at `T + 1ms`.
11. Last speaking timestamps and empty room maps are pruned on all permanent/error terminal paths.

## Rollback

The change has no migration. Revert the single implementation commit with its regression tests if release validation identifies a compatibility issue. Do not partially revert the repository transition contract without reverting dependent Hub serialization changes.
