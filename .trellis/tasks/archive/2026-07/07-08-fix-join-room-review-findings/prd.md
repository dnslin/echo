# Fix join-room review findings

## Goal

Fix the two confirmed PR #33 code-review findings in `POST /v1/rooms/join` so temporary-room joins preserve the MVP product invariants under concurrent requests and retained-empty-room recovery.

## Background

PR #33 added the join-room API for Issue #11. Review found two defects in the current join service path:

1. Capacity check and member insertion are separate repository calls, so concurrent joins can both observe capacity below 10 and both insert.
2. Joining a retained empty room before `expires_at` does not clear `last_empty_at` / `expires_at`, so later logic can expire a room that already has an online member.

Relevant source contracts:

- `prd.md`: a room with members stays valid; if someone joins within 30 minutes after the last member leaves, the room recovers; after 30 minutes the invite becomes invalid.
- `design.md`: join must reject invalid/expired/full rooms and clear the empty-room timer when a retained room recovers.
- `.trellis/spec/backend/database-guidelines.md`: repository owns GORM/SQLite transaction behavior for persisted room/member data.
- `.trellis/spec/backend/error-handling.md`: join public error mapping remains unchanged.

## Requirements

### R1 — Atomic join capacity enforcement

Joining a room must not admit more than 10 `online` or `reconnecting` members, including when multiple join requests run concurrently against the same room.

- Capacity counts only `online` and `reconnecting` members.
- `disconnected` members remain excluded.
- If the room is already at capacity at the point of insertion, the service returns `room.ErrRoomFull` and the HTTP layer continues to map it to `409 room_full`.

### R2 — Retained empty room recovery

When a join succeeds for an active room whose `last_empty_at` / `expires_at` fields are set but `expires_at` is still in the future, the persisted room must recover to an occupied active room:

- clear `last_empty_at`;
- clear `expires_at`;
- refresh `updated_at` to the join time;
- return a room snapshot reflecting the cleared expiry fields.

### R3 — Expired-room behavior remains unchanged

If the stored room state is `expired`, or `expires_at <= now`, join must still fail with `room.ErrRoomExpired`; if `expires_at <= now`, the repository must mark the room expired before returning the error.

### R4 — Existing join behavior remains unchanged

The fix must preserve existing Issue #11 behavior:

- normalized invite-code lookup;
- duplicate nicknames allowed;
- new joined member is non-host, `online`, unmuted, not speaking, `push_to_talk`;
- no LiveKit token, room session token, WebSocket data, room-owner controls, account checks, invite revocation, or reconnect-restore semantics added in this task.

## Diagnostic Hypotheses

1. If capacity checking and member insertion remain separate calls, concurrent requests can pass the same stale count. Moving the capacity decision and insert into one repository transaction should make the over-capacity test disappear.
2. If successful join does not update room lifecycle fields, retained rooms keep stale expiry metadata. Updating room recovery fields in the same join transaction should make the retained-room expiry test pass.
3. If HTTP mapping or invite validation caused the observed behavior, service/store regression tests would not reproduce it directly. Current evidence points to service/store transaction boundaries instead.

## Acceptance Criteria

- [ ] A regression test proves a room with 9 active members rejects one of two simultaneous joins and persists no more than 10 active members.
- [ ] A regression test proves a retained empty active room joined before `expires_at` clears `last_empty_at` and `expires_at` and returns a recovered room snapshot.
- [ ] Existing join-room service, store, and HTTP tests still pass.
- [ ] `go test -count=1 ./services/api/...` passes.
- [ ] `git diff --check` passes.

## Out of Scope

- Reconnect restore semantics for the same `anonymous_id`.
- Leave-room lifecycle implementation beyond the persisted fields used by this fix.
- LiveKit token issuance, room session tokens, or WebSocket state.
- Changing public HTTP error codes or Chinese product copy.
- Adding accounts, fixed rooms, room-owner controls, invite revocation, Redis/PostgreSQL, or external locking infrastructure.
