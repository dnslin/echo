# Design — Fix join-room review findings

## Architecture Boundary

The fix keeps the existing layer ownership:

- `room.Service` owns input validation, product errors, time/id generation, and high-level join decision flow.
- `store.Repository` owns SQLite/GORM transaction boundaries and persisted room/member mutations.
- `httpapi.Handlers` keep the existing request binding and error-envelope mapping; no public contract change is required.
- `invite` behavior is unchanged.

## Core Design

Introduce one repository operation for the successful join mutation instead of composing count and insert in the service:

```go
func (r *Repository) JoinRoomWithMember(ctx context.Context, room domain.Room, member domain.Member, activeStates []domain.MemberState, maxActiveMembers int, joinedAt time.Time) (domain.Room, error)
```

The operation runs in a single SQLite transaction:

1. Re-read the room row by `room.id` inside the transaction.
2. Reject missing rows as `domain.ErrRoomNotFound`.
3. Count current members whose state is in `activeStates`.
4. If count is already `>= maxActiveMembers`, return `domain.ErrRoomFull`.
5. Insert the service-built member.
6. If the room has retained-empty metadata (`last_empty_at` or `expires_at` non-nil), clear both fields and set `updated_at = joinedAt`.
7. Return the post-join room snapshot.

## Why this shape

Bedrock constraints:

- The capacity invariant is about durable rows in one SQLite database.
- Separate read and write calls cannot guarantee a single decision point under concurrent requests.
- Room recovery is a durable lifecycle transition, so it must be persisted with the join that makes the room occupied again.

A service-level mutex or HTTP-level guard would only protect one process and would not express the data invariant at the database boundary. A single store operation is the smallest mechanism that owns both affected durable facts.

## Error Mapping

- `domain.ErrRoomFull` is returned by the repository when the transaction observes capacity at the limit.
- `room.Service` maps repository `domain.ErrRoomFull` to existing `room.ErrRoomFull`.
- `domain.ErrRoomNotFound` remains mapped to `room.ErrInviteNotFound` for lookup failures; unexpected transaction disappearance can also map to not found.
- HTTP behavior remains unchanged through existing `writeRoomError`.

## Compatibility

Existing repository methods can remain for current tests and future direct use, but the service join path should use the new atomic method. OpenAPI does not change because the public request/response/error shape is unchanged.

## Tests

- Service unit test with a fake repository proves the service uses the atomic join method and returns the recovered room snapshot.
- Store integration tests prove retained empty room recovery clears expiry fields.
- Store concurrency regression test stresses simultaneous joins against SQLite and asserts no more than 10 active members persist and at least one caller receives `domain.ErrRoomFull`.
- Existing HTTP tests continue to validate public mapping.
