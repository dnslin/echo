# Implement — Issue 12 Leave Room Lifecycle

## Ordered checklist

### 1. Add domain/store lifecycle tests first

- [ ] Add store tests in `services/api/internal/store/join_test.go` or a new `leave_test.go`:
  - leaving an online member sets `state = disconnected` and `speaking = false`;
  - disconnected member is excluded by `CountRoomMembersByStates`;
  - non-last-member leave does not set room `last_empty_at` / `expires_at`;
  - last-member leave sets `last_empty_at = fixedNow`, `expires_at = fixedNow + 30m`, `updated_at = fixedNow`;
  - repeated leave does not extend an existing expiry;
  - `ExpireEmptyRooms` marks retained rooms expired when `expires_at <= now`;
  - defensive no-active-member old room expires when `created_at <= now - 30m`.
- [ ] Run the targeted store test and verify it fails for missing methods before implementing.

### 2. Implement store repository methods

- [ ] Add `domain.ErrMemberNotFound`; add `domain.ErrRoomExpired` only if store needs a domain-level expired sentinel.
- [ ] Add model-to-domain member conversion helper.
- [ ] Implement `Repository.LeaveRoomMember(ctx, roomID, memberID, activeStates, leftAt, retention)` with a single SQLite immediate transaction.
- [ ] Implement `Repository.ExpireEmptyRooms(ctx, now, retention)`.
- [ ] Keep existing `JoinRoomWithMember` behavior unchanged.
- [ ] Re-run store tests.

### 3. Add room service tests and service methods

- [ ] Add room service tests in `services/api/internal/room/service_test.go` / `service_join_test.go` or a new `service_leave_test.go`:
  - validation for blank `room_id` / `member_id`;
  - valid leave delegates active states and retention to repository;
  - missing room/member and expired room map to stable service errors;
  - `ExpireEmptyRoomsContext` uses controlled `now`.
- [ ] Extend the service repository interface with leave/expiry capabilities through small private interfaces.
- [ ] Add `LeaveInput`, `LeaveResult`, `LeaveContext`, and `ExpireEmptyRoomsContext`.
- [ ] Add `ErrMemberNotFound` at room-service level and map store/domain sentinels.
- [ ] Re-run room tests.

### 4. Add HTTP tests and handler route

- [ ] Add HTTP tests in `services/api/internal/http/handlers_test.go` or `handlers_leave_test.go`:
  - valid `POST /v1/rooms/{room_id}/leave` returns `204`;
  - request context propagates;
  - malformed/oversized body returns `400 invalid_request` before service call;
  - blank `member_id` maps to validation envelope;
  - missing room maps to `404 room_not_found`;
  - missing member maps to `404 member_not_found`;
  - expired room maps to existing `410 room_expired`;
  - integration: create room, leave host, join before expiry clears expiry fields;
  - integration: create/leave, move controlled time or seed due expiry, join after expiry returns `410 room_expired`.
- [ ] Extend `Handlers` with `roomLeaver`.
- [ ] Add `WithRoomLeaver` and register `/v1/rooms/:room_id/leave` when configured.
- [ ] Update `NewHandlers` signature carefully; existing create/join tests must compile.
- [ ] Re-run HTTP tests.

### 5. Update OpenAPI contract

- [ ] Add `/v1/rooms/{room_id}/leave` path.
- [ ] Add `LeaveRoomRequest` schema.
- [ ] Add documented `204`, `400`, `404`, `410`, `500` responses.
- [ ] Extend error-code enum with `room_not_found` and `member_not_found` if used.
- [ ] Check OpenAPI examples against handler behavior.

### 6. Full verification and Trellis checks

- [ ] Run:

```bash
go test -count=1 ./services/api/internal/store ./services/api/internal/room ./services/api/internal/http
go test -count=1 ./services/api/...
git diff --check
```

- [ ] Run the Trellis check skill/command required by the workflow.
- [ ] If any check fails, fix the cause and repeat until all pass.

## Files expected to change

- `services/api/internal/domain/types.go`
- `services/api/internal/store/sqlite.go`
- `services/api/internal/store/*_test.go`
- `services/api/internal/room/service.go`
- `services/api/internal/room/*_test.go`
- `services/api/internal/http/router.go`
- `services/api/internal/http/handlers.go`
- `services/api/internal/http/*_test.go`
- `services/api/openapi.yaml`

## Risk points

- `NewHandlers` and `NewRouter` currently accept create/join dependencies; adding leaver must not break tests that configure only one dependency.
- Existing join-room retained-empty recovery tests must remain passing.
- Repeated leave must not refresh expiry, or a retrying client could keep a room alive forever.
- Cleanup for created-but-unentered compatibility must not expire normal newly-created rooms with an online host member.
- Do not introduce auth/session/token assumptions; they are outside this issue.

## Rollback points

- Store transaction methods are the highest-risk change. If concurrency or transaction behavior regresses, revert only store changes and keep service/HTTP tests as the specification.
- OpenAPI changes are contract-only; if behavior changes, update code to match PRD/design rather than loosening docs.

## Branch and workflow steps

1. Ensure planning artifacts are reviewed and task status is moved to `in_progress` through Trellis Phase 1.4.
2. Update from latest `master` and create a branch named `issue-12-leave-room-lifecycle`.
3. Implement in the checklist order above.
4. Run verification and Trellis check.
5. Do not commit unless the user/workflow explicitly asks at finish phase.
