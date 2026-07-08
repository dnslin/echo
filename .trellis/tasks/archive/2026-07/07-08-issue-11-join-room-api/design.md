# Design: Join Temporary Room by Invite Code API

## Scope

This task extends the existing API service created by Issue #10. It only changes backend code under `services/api/**` and the HTTP contract file `services/api/openapi.yaml`.

## Architecture boundary

- `invite` owns code normalization and generated-code alphabet rules.
- `room.Service` owns product rules: input validation, expiry, capacity, host/non-host member creation, and product error semantics.
- `store.Repository` owns GORM/SQLite reads and writes for rooms and members.
- `httpapi.Handlers` owns request binding, response shape, HTTP status mapping, and JSON error envelope.
- `openapi.yaml` owns the public HTTP contract.

No LiveKit, WebSocket, room-session token, account, or room-owner behavior is introduced in this task.

## Data flow

```text
POST /v1/rooms/join
  -> Gin JSON binding with request-size cap
  -> room.Service.JoinContext(ctx, JoinInput)
    -> trim/validate anonymous_id, nickname, avatar_id
    -> invite.Normalize(invite_code)
    -> store.Repository.FindRoomByInviteCode(ctx, normalized_code)
    -> reject missing / expired / full
    -> build non-host online member
    -> store.Repository.CreateMember(ctx, member)
  -> HTTP 200 response with room + member
```

## Contracts

### Invite normalization

`invite.Normalize(input string) (string, error)` returns the canonical 6-character code or an error. It ignores whitespace and ASCII `-`, uppercases ASCII letters, and accepts only `A-Z0-9`.

Room service will decide whether an invalid input is empty or malformed so the HTTP layer can return the correct product message.

### Room service

Add:

```go
type JoinInput struct {
    InviteCode   string
    AnonymousID  string
    Nickname     string
    AvatarID     string
}

type JoinResult struct {
    Room   domain.Room
    Member domain.Member
}

func (s *Service) JoinContext(ctx context.Context, input JoinInput) (JoinResult, error)
func (s *Service) Join(input JoinInput) (JoinResult, error)
```

Add product sentinels in `room`:

- `ErrInviteNotFound`
- `ErrRoomExpired`
- `ErrRoomFull`

Keep `ValidationError` for request/product validation errors.

### Store repository

Add repository methods needed by the service:

```go
func (r *Repository) FindRoomByInviteCode(ctx context.Context, inviteCode string) (domain.Room, error)
func (r *Repository) CountRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) (int, error)
func (r *Repository) CreateMember(ctx context.Context, member domain.Member) error
func (r *Repository) MarkRoomExpired(ctx context.Context, roomID string, updatedAt time.Time) error
```

The store layer translates record-not-found to a stable domain/store-level sentinel so service code does not depend on raw GORM errors.

### HTTP

Add route:

```http
POST /v1/rooms/join
```

Request body:

```json
{
  "invite_code": "k7-m9 q2",
  "anonymous_id": "anon_local_456",
  "nickname": "Alice",
  "avatar_id": "avatar_08"
}
```

Success response uses the existing create-room response shape:

```json
{
  "room": { "id": "room_...", "invite_code": "K7M9Q2", "state": "active" },
  "member": { "id": "mem_...", "is_host": false, "state": "online" }
}
```

HTTP status mapping:

| Error | Status | Code | Message |
| --- | ---: | --- | --- |
| invalid JSON / oversized body | 400 | `invalid_request` | `请求格式无效` |
| empty invite | 400 | `empty_invite_code` | `请输入邀请码` |
| invalid invite format | 400 | `invalid_invite_format` | `邀请码应为 6 位字母或数字` |
| invite not found | 404 | `invite_not_found` | `邀请码无效，请检查后重试` |
| room expired | 410 | `room_expired` | `该房间已过期，请让朋友重新创建` |
| room full | 409 | `room_full` | `房间人数已满，暂时无法加入` |
| identity/display validation | 400 | existing create-room validation code | existing create-room message |
| unexpected error | 500 | `internal_error` | `服务器错误` |

## Compatibility

- `POST /v1/rooms` response shape remains unchanged.
- Existing create-room validation codes/messages remain unchanged.
- Existing database schema remains compatible; no new table is required.
- `domain.MemberState` gains `reconnecting` and `disconnected` constants so capacity checks can count `online` and `reconnecting`, but existing persisted `online` rows remain valid.

## Capacity and concurrency note

The product invariant is a room maximum of 10 online/reconnecting members. MVP is a single API instance backed by SQLite. The service should keep the count-and-create flow simple and testable; if a later review identifies concurrent over-admission as a real risk, the store method can be tightened into one transaction without changing the HTTP contract.

## Rollback shape

If the join path fails review, revert the changes to:

- `services/api/internal/invite/service.go` and tests;
- `services/api/internal/domain/types.go`;
- `services/api/internal/store/**` repository additions and tests;
- `services/api/internal/room/**` join additions and tests;
- `services/api/internal/http/**` join route/handler/tests;
- `services/api/openapi.yaml` join contract.

Create-room code should remain recoverable because the existing public create contracts are preserved.
