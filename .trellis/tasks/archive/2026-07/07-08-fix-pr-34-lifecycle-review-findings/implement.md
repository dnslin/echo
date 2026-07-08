# Implement: Fix PR 34 lifecycle review findings

## Feedback loop

Use fast targeted Go package tests first, then full backend tests:

```bash
go test -count=1 ./services/api/internal/store ./services/api/internal/room ./services/api/internal/http
go test -count=1 ./services/api/...
git diff --check
```

## Ranked hypotheses

1. If expiry is decided at the service edge from `expires_at` alone, then a due retained room with an active member will be marked expired before the store can observe active count. Moving the due-expiry decision into `JoinRoomWithMember` should make the active-member join test pass.
2. If `JoinRoomWithMember` trusts the room snapshot passed by `JoinContext`, then a transaction-observed `expired` room can still accept a member. Rechecking `roomModel.State` in the transaction should make the expired-room store regression pass.
3. If last-active leave only starts retention when old empty metadata is nil, then stale/partial metadata prevents a fresh 30-minute window. Removing the nil-metadata gate while keeping `wasActive` should make the stale-retention leave regression pass without extending repeated leaves.
4. If cleanup uses a deferred transaction, then join/recovery can interleave between active count and state update. Using `BEGIN IMMEDIATE` for cleanup should serialize lifecycle writers and preserve the zero-active invariant.

## Execution checklist

1. Add failing regression tests before code changes:
   - service join does not expire due retained rooms with active members;
   - store join rejects due retained empty rooms and transaction-observed expired rooms;
   - store leave refreshes stale/partial retention on last-active leave;
   - store repeated leave after due retention returns expired without refreshing retention.
2. Run targeted tests and confirm failures match the review findings.
3. Implement store lifecycle fixes in `services/api/internal/store/sqlite.go`.
4. Implement service join fix in `services/api/internal/room/service.go` and update fakes/tests.
5. Re-run targeted tests until passing.
6. Run full backend tests and `git diff --check`.
7. Update backend spec if the fix changes a reusable contract.

## Review gates

- No new auth/session/LiveKit/WebSocket scope.
- No wall-clock sleeps in tests.
- Keep lifecycle state ownership in store transactions.
- Do not let repeated leave extend retention.
- Do not expire rooms with active members.
