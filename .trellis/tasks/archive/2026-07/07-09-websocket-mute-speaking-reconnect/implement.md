# Issue 16 Implementation Plan

## Goal

在现有 Issue #15 WebSocket 基础上，实现房间成员的 `mute`、`speaking`、`reconnect` 状态广播，并保证 reconnecting 成员继续计入容量、speaking 不改变成员排序、显式 leave 不误走 reconnect 流程。

## Delivery boundary

This task implements only:
- `member.mute_changed`
- `member.speaking_changed`
- `member.reconnecting`
- `member.restored`
- `member.disconnected`

This task does not implement:
- `member.voice_mode_changed`
- `free_talk`
- frontend / desktop UI
- LiveKit reconnect logic
- cross-instance fan-out

## Pre-flight facts

- Current branch `issue-16-websocket-state-broadcast` is presently not diverged from `master`.
- Execution should still re-check branch freshness immediately before coding. If `master` has moved by then, sync/rebase first so implementation starts from the latest mainline state.

## Ordered checklist

### 0. Reconfirm execution baseline before code

- [ ] Re-check current branch against `master` right before implementation starts.
- [ ] Set task metadata correctly:
  - branch = `issue-16-websocket-state-broadcast`
  - base branch = `master`
- [ ] Load current task context in this order during execution:
  - `implement.jsonl`
  - `prd.md`
  - `design.md`
  - `implement.md`

Validation:

```bash
git branch --show-current
git log --oneline master..HEAD
git log --oneline HEAD..master
```

### 1. Add repository mutation coverage first

Target files:
- `services/api/internal/store/sqlite.go`
- `services/api/internal/store/leave_test.go`
- `services/api/internal/store/sqlite_test.go`
- optional new focused tests if the existing files become too crowded

Work:
- [ ] Add/extend repository methods for member-state writes needed by this issue.
- [ ] Keep `reconnect_until` out of SQLite schema.
- [ ] Reuse existing durable leave/disconnect lifecycle logic for timeout-to-disconnected behavior.
- [ ] Add tests for:
  - mute update round-trip
  - speaking update round-trip
  - timeout disconnect preserves existing leave retention behavior
  - no ordering/capacity regression

Validation:

```bash
go test -count=1 ./services/api/internal/store/...
```

Rollback point:
- if repository shape becomes convoluted, split timeout-disconnect into a thin wrapper over existing leave mutation rather than duplicating lifecycle SQL.

### 2. Add room-service product-state methods and tests

Target files:
- `services/api/internal/room/service.go`
- `services/api/internal/room/service_credentials_test.go`
- new tests such as `service_state_test.go` / `service_disconnect_test.go` if cleaner

Work:
- [ ] Add room-service methods for member mute/speaking updates.
- [ ] Add service-owned rules:
  - muted member cannot become `speaking=true`
  - disconnected member cannot mutate state
  - timeout disconnect maps to correct durable lifecycle behavior
- [ ] Keep transport-only reconnect deadline logic out of room service.

Validation:

```bash
go test -count=1 ./services/api/internal/room/...
```

Review gate:
- room service should own product rules; ws hub should not decide business validity beyond transport parsing/throttling.

### 3. Extend WebSocket hub contract implementation

Target files:
- `services/api/internal/ws/hub.go`
- `services/api/internal/ws/hub_test.go`
- `services/api/cmd/api/main.go`

Work:
- [ ] Extend hub config with the room-state mutator dependency.
- [ ] Support established-connection commands:
  - `member.mute_changed`
  - `member.speaking_changed`
- [ ] Add room-local reconnect runtime state:
  - deadline
  - timer
  - cleanup on restore/timeout/room prune
- [ ] Distinguish close reasons:
  - explicit leave / replaced connection => no reconnect flow
  - unexpected close / heartbeat timeout / slow-consumer close => reconnect flow
- [ ] On unexpected disconnect:
  - clear speaking state when needed
  - broadcast `member.reconnecting`
  - preserve room slot and sequence rules
- [ ] On reconnect within window:
  - accept same authorized member
  - send snapshot
  - broadcast `member.restored`
- [ ] On timeout:
  - durable disconnect through room service
  - broadcast `member.disconnected`
- [ ] Overlay reconnecting state into snapshot projection without changing persisted ordering source.
- [ ] Add speaking dedupe/throttle guard with deterministic behavior.

Validation:

```bash
go test -count=1 ./services/api/internal/ws/...
```

Review gate:
- snapshot `seq` rules must stay intact;
- private messages must not advance shared room sequence;
- reconnecting overlay must not reorder members.

### 4. Add or update integration coverage at router/runtime boundary

Target files:
- `services/api/internal/http/router_ws_test.go`
- `services/api/internal/ws/hub_test.go`
- `services/api/cmd/api/main.go`

Work:
- [ ] Extend integration tests for:
  - mute broadcast between two clients
  - speaking broadcast between two clients
  - speaking updates without member reordering
  - unexpected disconnect enters reconnecting
  - reconnect within 30s restores same member ID
  - timeout removal emits `member.disconnected`
  - reconnecting member still counts toward room capacity
  - explicit HTTP leave still emits `member.left`, not reconnecting
- [ ] Keep existing handshake, snapshot, join/leave, and heartbeat tests passing.

Validation:

```bash
go test -count=1 ./services/api/internal/http/... ./services/api/internal/ws/...
```

### 5. Contract drift check

Target files:
- `docs/api/websocket.md` only if implementation semantics require clarification

Work:
- [ ] Compare implementation behavior against the existing contract.
- [ ] Update docs only if a concrete mismatch or missing clarification is discovered.
- [ ] Do not broaden the contract into `voice_mode_changed` / `free_talk` in this task.

Validation:

```bash
git diff -- docs/api/websocket.md
```

### 6. Full backend verification

- [ ] Run full backend test suite.
- [ ] Run whitespace/conflict check.
- [ ] If any test regresses from Issue #15 / earlier room lifecycle work, fix before proceeding.

Validation:

```bash
go test -count=1 ./services/api/...
git diff --check
```

### 7. Trellis quality pass before start/finish transitions

- [ ] Run `trellis-check` after implementation.
- [ ] If `trellis-check` finds issues, fix and rerun until green.
- [ ] Re-evaluate whether this task created new reusable backend knowledge worth writing into `.trellis/spec/` during Phase 3.3.

## Requirement-to-test matrix

| Requirement | Planned test coverage |
| --- | --- |
| mute broadcast | ws integration + repository round-trip |
| speaking broadcast | ws integration + repository/service tests |
| speaking does not reorder | ws integration snapshot/member-order assertions |
| disconnect enters reconnecting | ws integration |
| restore within 30s | ws integration |
| timeout removal after 30s | ws integration + durable lifecycle assertion |
| reconnecting counts toward capacity | room/store tests + ws integration |
| explicit leave stays leave | existing leave integration + new regression |
| illegal/out-of-scope messages do not mutate state | ws integration |

## Risky files / rollback notes

Most risky files:
- `services/api/internal/ws/hub.go`
- `services/api/internal/room/service.go`
- `services/api/internal/store/sqlite.go`

Why risky:
- They sit on the API/service/store boundary.
- Reconnect semantics mix in-memory transport state with durable lifecycle state.
- Sequence ordering and connection-close behavior are easy to break subtly.

Rollback strategy:
1. revert hub reconnect logic first if sequence/close behavior regresses;
2. keep repository mutation helpers if they remain small and correct;
3. never ship a state where explicit leave triggers reconnecting.

## Done definition

This task is ready for implementation completion only when all of the following are true:

- `prd.md`, `design.md`, `implement.md` are approved
- mute/speaking/reconnect acceptance criteria are covered by automated tests
- `go test -count=1 ./services/api/...` passes
- `git diff --check` passes
- `trellis-check` passes after fixes
- no accidental scope expansion into `voice_mode_changed` / `free_talk`
