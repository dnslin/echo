# Design: Fix PR 34 lifecycle review findings

## First-principles analysis

### Challenge assumptions

- Unverified convention: an edge/service pre-check can safely decide expiry before a store mutation. This is false under concurrent writes because the store is the only place that can observe current state and active-member count atomically.
- Unverified convention: non-nil `last_empty_at` / `expires_at` always means the room is currently empty. The review found stale metadata paths, so the code must treat active-member count as the stronger fact.
- Potentially wrong shortcut: `expires_at <= now` alone means expired. The project spec says empty rooms expire after retention; active `online` / `reconnecting` members are the durable evidence that the room is not empty.

### Bedrock truths

- SQLite can serialize lifecycle writes when the repository uses one immediate-write transaction.
- A member row in `online` or `reconnecting` state is the persisted active-member fact for MVP capacity and empty-room lifecycle.
- A room cannot be safely expired as an empty room unless the same durable decision observes zero active members.
- A repeated leave by a disconnected member is not a new transition from non-empty to empty and cannot create a new retention window.
- A last-active leave is the exact transition that starts the retention clock, regardless of stale metadata from earlier states.

### Rebuild from truths

- The service may validate inputs and map sentinels, but it must not decide due-retained-room expiry from a stale `FindRoomByInviteCode` snapshot.
- `JoinRoomWithMember` becomes the authoritative successful-join lifecycle gate: reload room under `BEGIN IMMEDIATE`, count active members, reject expired/due-empty rooms, insert the member, and clear retained metadata in one decision point.
- `LeaveRoomMember` updates the member first, then if the leaving member was active and no active members remain, writes a fresh `last_empty_at` / `expires_at` from `leftAt` without relying on old metadata being nil.
- `ExpireEmptyRooms` uses the same immediate-write transaction shape so cleanup cannot interleave between an active-count observation and its room-state update.

### Contrast with convention

Conventional edge-precheck thinking would keep expiry logic in `JoinContext` because it is close to HTTP behavior. That is suboptimal here: the decisive facts are persisted room state, active-member count, and the member insert, and only the repository transaction can observe and mutate them atomically.

### Conclusion

Lifecycle expiry must be owned by the store transaction, not by a pre-mutation service snapshot. Active-member count beats stale empty metadata.

## Technical design

### Service layer

- `JoinContext` still validates input, resolves invite code, rejects a room already returned as `expired`, builds the member, and maps repository sentinels.
- Remove the `expires_at <= now -> MarkRoomExpired` service-edge branch.
- Keep `ErrRoomExpired` mapping when `JoinRoomWithMember` returns `domain.ErrRoomExpired`.

### Store layer

- `JoinRoomWithMember` under `BEGIN IMMEDIATE`:
  1. reload room by ID;
  2. reject `state == expired` with `domain.ErrRoomExpired`;
  3. count active members using caller-provided states;
  4. if `expires_at <= joinedAt` and active count is zero, mark room expired with `updated_at = joinedAt`, commit, and return `domain.ErrRoomExpired`;
  5. reject capacity if active count is already at `maxActiveMembers`;
  6. insert member;
  7. clear stale/retained empty metadata on success and return the recovered room snapshot.

- `LeaveRoomMember` under `BEGIN IMMEDIATE`:
  1. reload room/member;
  2. reject persisted `expired` rooms;
  3. if the member is already disconnected and the room is due retained with zero active members, mark expired and return `domain.ErrRoomExpired`;
  4. update leaving member to `disconnected`, `speaking=false`;
  5. if the member was active and no active members remain after update, write fresh `last_empty_at = leftAt`, `expires_at = leftAt + retention`, and `updated_at = leftAt` regardless of previous metadata.

- `ExpireEmptyRooms`:
  - switch from GORM default transaction to explicit `BEGIN IMMEDIATE` connection scope, matching join/leave lifecycle writes.
  - keep zero-active-member predicate before expiring retained or old no-active rooms.

## Compatibility

- No HTTP/OpenAPI contract changes.
- Existing room/member models remain unchanged.
- Existing leave idempotency is preserved for not-yet-due retained rooms.
- Existing room-session auth non-goal remains unchanged.

## Risks and mitigations

- Risk: changing service expiry ownership can leave due empty rooms active if the repository does not handle it. Mitigation: add store regression test for due empty join rejection and persisted `expired` state.
- Risk: stale metadata cleanup can accidentally extend repeated leaves. Mitigation: only refresh retention when `wasActive == true` and post-update active count is zero.
- Risk: explicit SQLite transaction handling can leave rollback paths. Mitigation: reuse the existing join/leave `committed` defer shape and cover tests.
