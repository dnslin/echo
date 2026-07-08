# Implement — Fix join-room review findings

## Checklist

1. Add a focused feedback loop:
   - retained-empty-room join regression test;
   - concurrent join capacity regression test at the store/service seam.
2. Add a repository contract for atomic successful joins.
3. Implement the GORM/SQLite transaction:
   - nil repository/DB guard;
   - row re-read by room ID;
   - active-state count;
   - `domain.ErrRoomFull` at capacity;
   - member insert;
   - clear `last_empty_at` / `expires_at` and update room timestamp when recovering;
   - return post-join room snapshot.
4. Update `room.Service.JoinContext` to call the atomic repository method and map `domain.ErrRoomFull` to `room.ErrRoomFull`.
5. Preserve existing expired-room pre-check and `MarkRoomExpired` behavior.
6. Update backend specs if the new repository contract is a reusable implementation rule.
7. Run validation:
   - `go test -count=1 ./services/api/...`
   - `git diff --check`

## Risk Points

- SQLite concurrency behavior in tests can be timing-sensitive. Keep the regression deterministic by starting goroutines from a barrier and asserting final persisted count, not request ordering.
- Do not add global process locks; the invariant must be owned by persistence.
- Do not change public HTTP response copy or status codes.

## Rollback

If the atomic repository contract proves too broad, keep the service behavior unchanged and instead move only the count/insert/recovery mutation into a narrower store method used exclusively by join-room.
