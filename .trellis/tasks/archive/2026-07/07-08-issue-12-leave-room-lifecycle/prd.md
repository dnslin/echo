# Issue 12 — Leave Room and Empty-Room 30-Minute Retention

## Goal

Implement GitHub Issue #12 for the backend API service: members can leave a temporary room, the member is removed from the active online/reconnecting capacity set, the last active member leaving starts a 30-minute empty-room retention window, rejoining before expiry recovers the room through the existing join path, and expired empty rooms reject the original invite code.

User value: a temporary room behaves like the MVP product promise instead of becoming a permanent invite-code room after everyone leaves.

## Background and confirmed facts

- GitHub Issue #12 is `[S07] 离开房间与空房 30 分钟保留`, part of #3, and was blocked by #11.
- GitHub Issue #11 is closed, and current `master` contains the join-room vertical path.
- Current code already supports:
  - `POST /v1/rooms` create-room with persisted room + host member;
  - `POST /v1/rooms/join` join-room with invite normalization, capacity check, expired-room rejection, and retained-empty-room recovery;
  - SQLite `rooms.last_empty_at` / `rooms.expires_at` fields;
  - `members.state` values `online`, `reconnecting`, and `disconnected`.
- Existing #10 acceptance requires newly created rooms to return a host member and have `last_empty_at = nil` / `expires_at = nil`; this task must not break that contract.
- ADR `docs/adr/0014-business-service-room-lifecycle.md` makes the business service authoritative for lifecycle: last member leaving sets `last_empty_at` and `expires_at = now + 30 minutes`; rejoin before expiry clears those fields; join after expiry marks the room expired.
- Existing join implementation already clears `last_empty_at` / `expires_at` on successful join before expiry and marks expired rooms when `expires_at <= now`.

## Requirements

### R1 — Leave command API

Add a backend HTTP command endpoint:

```http
POST /v1/rooms/{room_id}/leave
Content-Type: application/json
```

Request body:

```json
{
  "member_id": "mem_abc"
}
```

The endpoint must:

- pass `c.Request.Context()` into the room service;
- cap request-body bytes before binding, using the same request-size limit as create/join unless a later spec changes all command endpoints;
- return `204 No Content` when the leave mutation succeeds;
- return the standard JSON error envelope for malformed input, missing room/member, expired room, or unexpected failures.

### R2 — Member leaves active capacity set

Leaving a room must change the existing member row so it no longer counts as active:

- set `members.state = disconnected`;
- set `members.speaking = false`;
- keep the row for history and future debugging;
- ensure existing capacity queries continue to exclude disconnected members.

A repeated leave for an already-disconnected existing member should be idempotent: the desired end state is already true, so it should not create a second empty-room timer or return a misleading server error.

### R3 — Last active member starts empty-room retention

When the leave mutation causes the room to have zero active members (`online` or `reconnecting`):

- set `rooms.last_empty_at = now`;
- set `rooms.expires_at = now + 30 minutes`;
- set `rooms.updated_at = now`;
- keep `rooms.state = active` until expiry is reached.

When other active members remain, leave must not set or refresh `last_empty_at` / `expires_at`.

### R4 — Rejoin before expiry recovers room

Existing join behavior must continue to clear retained-empty metadata when a user joins before `expires_at`:

- `last_empty_at` becomes `nil`;
- `expires_at` becomes `nil`;
- `updated_at` becomes join time;
- returned room snapshot reflects the recovered active room.

This is mostly already covered by the current join path; #12 must preserve and, if needed, extend regression coverage around leave-created retained rooms.

### R5 — Expire empty rooms after 30 minutes

Add a service/store path that marks active rooms expired when their `expires_at <= now`.

The behavior must be testable with a controlled time source and must not depend on wall-clock sleeps.

After expiry:

- `rooms.state = expired`;
- original invite code cannot be used to join;
- `POST /v1/rooms/join` returns the existing `410 room_expired` mapping and Chinese message `该房间已过期，请让朋友重新创建`.

### R6 — Created-but-unentered room compatibility

The product PRD says a room created but not entered within 30 minutes should expire. Current #10 code has already chosen a create-room contract that persists the creator as an online host member and returns `last_empty_at = nil` / `expires_at = nil` on creation. This task must not rewrite that merged contract.

To preserve compatibility while covering the lifecycle invariant, expiry cleanup may also expire active rooms that have no active members and are older than the 30-minute retention window. This covers defensive data states or future UI flows that create a room row before any active member exists, without breaking the existing create-room response.

### R7 — API contract documentation

Update `services/api/openapi.yaml` to document:

- `POST /v1/rooms/{room_id}/leave` request body;
- `204` success response;
- public error responses and codes;
- unchanged create/join response shapes.

## Out of scope

- Room-owner close-room, kick, transfer, or invite revocation.
- Fixed rooms or long-term invite retention.
- WebSocket broadcast events such as `member.left`.
- Reconnect-window implementation beyond respecting existing `reconnecting` state in capacity/active-member counts.
- LiveKit participant removal, LiveKit token issuance, room session tokens, or media cleanup.
- Frontend leave-room UI.
- Changing #10 create-room contract that currently creates an online host member.

## Acceptance criteria

- [ ] AC1: `POST /v1/rooms/{room_id}/leave` with a valid active member returns `204`.
- [ ] AC2: After a member leaves, their persisted state is `disconnected`, `speaking = false`, and they are excluded from `online`/`reconnecting` capacity counts.
- [ ] AC3: If other active members remain, leave does not set `last_empty_at` or `expires_at` on the room.
- [ ] AC4: When the last active member leaves, the room remains `active` and records `last_empty_at = now` plus `expires_at = now + 30 minutes`.
- [ ] AC5: Joining before `expires_at` succeeds through the existing join endpoint and clears `last_empty_at` / `expires_at`.
- [ ] AC6: Once `expires_at <= now` is processed or observed by join, the room becomes expired and the original invite code returns `410 room_expired`.
- [ ] AC7: A created room with no active members and age >= 30 minutes can be expired by the cleanup path without breaking the normal create-room initial `expires_at = nil` contract.
- [ ] AC8: Leave HTTP validation/product errors use the standard JSON error envelope and do not expose raw database errors.
- [ ] AC9: OpenAPI documents the leave endpoint request, `204` response, and all exposed error codes.
- [ ] AC10: Time-related tests use a controlled time source, not sleeps.
- [ ] AC11: `go test -count=1 ./services/api/...` passes.
- [ ] AC12: `git diff --check` passes.

## Requirement-to-test mapping

| Requirement | Test coverage |
| --- | --- |
| R1 | HTTP leave success, malformed/oversized request, missing member/room tests |
| R2 | store/service leave tests assert `disconnected`, `speaking=false`, active count exclusion |
| R3 | service/store tests for non-last-member and last-member leave |
| R4 | integration test: leave last member, join before expiry, expiry fields clear |
| R5 | service/store cleanup tests plus join-after-expiry HTTP/service test |
| R6 | cleanup test for active room with no active members and old `created_at` |
| R7 | OpenAPI review plus grep/check for documented path and error codes |

## First-principles analysis

### Challenge assumptions

- Unverified assumption: leave should delete member rows. That would erase useful lifecycle facts and is unnecessary because capacity already depends on state.
- Unverified assumption: empty-room expiry must be implemented by background goroutine now. The irreducible product need is a deterministic mutation/cleanup path; a scheduler can call it later.
- Potentially wrong assumption: #12 should rewrite create-room so no host member exists until a separate “enter room” command. That conflicts with the already merged #10 create-room contract and would break existing userspace/tests.
- Analogy-based assumption: authentication/session token must exist before leave. Current MVP backend slice has no room session token yet, and #12 only asks for lifecycle; adding auth/token would expand scope beyond issue boundaries.

### Bedrock truths

- The authoritative lifecycle data is in SQLite `rooms` and `members`.
- A member counts as active only while `state` is `online` or `reconnecting`; `disconnected` does not count.
- A room with at least one active member must not be expired as empty.
- The last transition from active-member count > 0 to 0 is the only moment that starts the 30-minute retention timer.
- `expires_at <= now` is enough to decide that an empty retained room is expired.
- Tests can control `now`; they do not need real time to pass.

### Rebuilt conclusion

The smallest complete mechanism is: service validates `room_id`/`member_id`, repository performs one SQLite transaction that marks the member disconnected and starts the room timer only if no active members remain, existing join recovers retained rooms before expiry, and a deterministic cleanup method marks expired rooms. This satisfies #12 without adding account/session/media/WebSocket complexity and without breaking #10/#11 contracts.

## Open questions

None blocking. Repository evidence and existing product/ADR/task history answer the planning-relevant decisions.
