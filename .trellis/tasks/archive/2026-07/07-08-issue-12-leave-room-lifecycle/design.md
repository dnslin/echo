# Design — Issue 12 Leave Room Lifecycle

## Architecture boundary

This task stays inside the API service.

- `services/api/internal/http`: transport binding, request size cap, JSON error envelope, HTTP status mapping, OpenAPI-aligned response behavior.
- `services/api/internal/room`: product validation, controlled time source, product sentinel errors, lifecycle orchestration.
- `services/api/internal/store`: SQLite/GORM transaction boundaries and durable mutations for members and rooms.
- `services/api/internal/domain`: existing room/member states remain sufficient; no new state is required.
- `services/api/openapi.yaml`: public HTTP contract.

No desktop, LiveKit, WebSocket, account, room-owner, or frontend code is part of this task.

## First-principles design

### Irreducible facts

1. A temporary room's validity is determined by business rows, not LiveKit rooms.
2. A member being online is not the row's existence; it is the member `state`.
3. A room becomes empty when active member count over `online` + `reconnecting` reaches zero.
4. `expires_at = last_empty_at + 30 minutes` is a deterministic value; tests can assert it exactly.
5. If a join before expiry clears empty metadata, the room is no longer empty and must not be expired by stale metadata.
6. Current create-room behavior persists an online host member and must remain backward-compatible.

### Rebuilt mechanism

Implement one successful leave mutation at the repository boundary:

```go
func (r *Repository) LeaveRoomMember(ctx context.Context, roomID string, memberID string, activeStates []domain.MemberState, leftAt time.Time, retention time.Duration) (domain.Room, domain.Member, error)
```

The repository operation owns the durable invariant because it must update both `members` and `rooms` consistently.

Transaction steps:

1. Start an immediate SQLite transaction through the same `BEGIN IMMEDIATE` pattern used by `JoinRoomWithMember`.
2. Load the room by `roomID`.
3. Reject missing room as `domain.ErrRoomNotFound`.
4. If room state is `expired`, return `domain.ErrRoomExpired`.
5. Load the member by `roomID + memberID`.
6. Reject missing member as `domain.ErrMemberNotFound` or equivalent store/domain sentinel.
7. If the member is not already disconnected, set `state = disconnected`, `speaking = false`.
8. Count remaining active members in `activeStates` after the member update.
9. If the count is zero and the room does not already have an empty timer, set `last_empty_at = leftAt`, `expires_at = leftAt + retention`, `updated_at = leftAt`.
10. If active members remain, leave room empty metadata unchanged.
11. Commit and return the updated room and member snapshot.

Idempotency:

- If the member already is `disconnected`, return success with the existing disconnected state.
- Do not refresh `last_empty_at` / `expires_at` for a repeated leave, because repeated client retry must not extend the empty-room lifetime.

## Domain/service API

Add service contracts:

```go
type LeaveInput struct {
    RoomID   string
    MemberID string
}

type LeaveResult struct {
    Room   domain.Room
    Member domain.Member
}

func (s *Service) LeaveContext(ctx context.Context, input LeaveInput) (LeaveResult, error)
func (s *Service) ExpireEmptyRoomsContext(ctx context.Context) (int, error)
```

Service responsibilities:

- Trim and validate `RoomID` and `MemberID`.
- Return `ValidationError` for blank IDs.
- Require repository support for leave and expiry operations.
- Pass `now.UTC()` and `30 * time.Minute` into the repository.
- Map store/domain missing-member errors to a stable room-service sentinel for HTTP mapping.

Suggested room-level sentinels:

```go
var ErrMemberNotFound = errors.New("member not found")
```

Existing sentinels remain:

- `ErrInviteNotFound`
- `ErrRoomExpired`
- `ErrRoomFull`

## Store/repository additions

Add helpers while keeping existing public repository contracts intact:

```go
func (r *Repository) LeaveRoomMember(ctx context.Context, roomID string, memberID string, activeStates []domain.MemberState, leftAt time.Time, retention time.Duration) (domain.Room, domain.Member, error)
func (r *Repository) ExpireEmptyRooms(ctx context.Context, now time.Time, retention time.Duration) (int, error)
```

`ExpireEmptyRooms` should mark active rooms expired when either:

1. `expires_at IS NOT NULL AND expires_at <= now`; or
2. defensive compatibility case: `expires_at IS NULL`, room is active, `created_at <= now - retention`, and there are zero active (`online` or `reconnecting`) members.

The second case covers created-but-unentered data states without changing the current create-room contract that creates an online host member.

Store error mapping:

- Add `domain.ErrMemberNotFound` if needed so store does not leak `gorm.ErrRecordNotFound`.
- Reuse `domain.ErrRoomNotFound` and `domain.ErrRoomFull`.
- Add `domain.ErrRoomExpired` only if needed at store boundary; alternatively the repository can return room-service mapping via domain sentinel. Keep HTTP independent of GORM.

## HTTP contract

Add router option and route:

```go
type roomLeaver interface {
    LeaveContext(ctx context.Context, input room.LeaveInput) (room.LeaveResult, error)
}

func WithRoomLeaver(roomLeaver roomLeaver) RouterOption

v1.POST("/rooms/:room_id/leave", handlers.LeaveRoom)
```

Request:

```json
{
  "member_id": "mem_abc"
}
```

Success:

```http
204 No Content
```

Error mapping:

| Condition | Status | Code | Message |
| --- | --- | --- | --- |
| malformed/oversized JSON | 400 | `invalid_request` | `请求格式无效` |
| blank `room_id` or `member_id` | 400 | service validation code | service validation message |
| room missing | 404 | `room_not_found` | `房间不存在或已失效` |
| member missing from room | 404 | `member_not_found` | `成员不在房间中` |
| room expired | 410 | `room_expired` | `该房间已过期，请让朋友重新创建` |
| unexpected failure | 500 | `internal_error` | `服务器错误` |

The new missing-room/member copy is scoped to backend lifecycle tests because the root PRD does not yet define leave-specific errors.

## OpenAPI changes

Update `services/api/openapi.yaml`:

- add `/v1/rooms/{room_id}/leave`;
- add `LeaveRoomRequest` schema;
- extend `ErrorResponse.error.code` enum with `room_not_found` and `member_not_found` if those are exposed;
- keep existing create/join examples unchanged.

## Compatibility

- Existing `/healthz`, create-room, join-room routes and responses remain unchanged.
- Existing #10 tests that assert created rooms have nil expiry fields remain valid.
- Existing #11 join behavior remains valid; leave-created retained rooms reuse that recovery path.
- No schema migration beyond existing AutoMigrate fields is needed.
- No production data is assumed, but the defensive cleanup path handles rows with no active members and no expiry metadata.

## Test scenarios

### Service tests

- Leave with blank room/member IDs returns validation errors.
- Leave non-last active member marks that member disconnected but does not set room expiry fields.
- Leave last active member sets exact `last_empty_at` and `expires_at = now + 30m` using controlled `now`.
- Repeated leave does not extend existing empty-room expiry.
- Leave missing room/member maps to stable service errors.
- `ExpireEmptyRoomsContext` marks due rooms expired and ignores rooms before due time or with active members.

### Store tests

- `LeaveRoomMember` marks member disconnected and clears speaking.
- Active-member count excludes the leaving member after transaction.
- Last-member leave starts retention.
- Non-last-member leave does not start retention.
- `ExpireEmptyRooms` marks due retained rooms expired.
- Defensive created-but-unentered case expires only when no active members exist.

### HTTP tests

- `POST /v1/rooms/{room_id}/leave` returns `204` for a valid member.
- After leave, a new member can join before expiry and response has `last_empty_at = null`, `expires_at = null`.
- After expiry is reached, join returns `410 room_expired`.
- Malformed/oversized request does not call the service.
- Missing room/member maps to documented JSON errors.
- Request context is propagated.

## Verification commands

```bash
go test -count=1 ./services/api/internal/store ./services/api/internal/room ./services/api/internal/http
go test -count=1 ./services/api/...
git diff --check
```

Trellis check must run after implementation; any failure must be fixed and rerun until passing.
