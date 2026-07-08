# Fix PR 34 lifecycle review findings

## Goal

Fix the confirmed PR #34 lifecycle review findings for the Issue #12 leave-room implementation so room join, leave, and empty-room expiry preserve the same durable lifecycle invariant under stale metadata and concurrent lifecycle operations.

## Requirements

- R1. Join must not expire a room only because `expires_at <= now` when the room still has any active member (`online` or `reconnecting`).
- R2. Successful join must make the expiry decision inside the store-owned write transaction that also counts active members, inserts the member, and clears retained-empty metadata.
- R3. Join must reject and mark expired a due retained room only when the transaction observes zero active members.
- R4. Join must not insert a member into a room that the transaction observes as already `expired`.
- R5. Last-active-member leave must start a fresh 30-minute retention window even when stale or partial empty metadata already exists; repeated leave by an already disconnected member must not extend retention.
- R6. Empty-room cleanup must not race with join/recovery in a way that leaves active members in an expired room.
- R7. Keep the fix limited to backend lifecycle correctness. Do not add room-session auth, LiveKit participant removal, WebSocket broadcasts, scheduler wiring, or broad performance rewrites in this task.

## Acceptance Criteria

- [ ] Regression tests cover a due retained room with an active member: join succeeds, does not call broad expiry, and clears stale empty metadata.
- [ ] Regression tests cover a due retained room with zero active members: join returns `ErrRoomExpired`, does not insert a member, and persists `room.state = expired`.
- [ ] Regression tests cover `JoinRoomWithMember` rejecting a transaction-observed expired room before member insert.
- [ ] Regression tests cover last-active leave refreshing stale/partial retention metadata to `leftAt + 30m`.
- [ ] Regression tests cover repeated leave after a due retained empty room returning the stable expired-room behavior without extending retention.
- [ ] Store/service tests for lifecycle behavior pass with controlled time; no sleeps or wall-clock timing assertions.
- [ ] `go test -count=1 ./services/api/internal/store ./services/api/internal/room ./services/api/internal/http` passes.
- [ ] `go test -count=1 ./services/api/...` passes.
- [ ] `git diff --check` passes.

## Non-goals

- No new authentication/session boundary; PR #34 explicitly scoped room-session tokens out.
- No LiveKit media cleanup or participant removal.
- No runtime scheduler wiring for `ExpireEmptyRoomsContext` beyond making the repository method safe when invoked.
- No broad database indexing or cleanup-query performance refactor unless required by the correctness fix.
