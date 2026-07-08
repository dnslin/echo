# Database Guidelines

> Database patterns and conventions for the echo API service.

---

## Scenario: API SQLite persistence with GORM

### 1. Scope / Trigger

- Trigger: adding or modifying API service persistence under `services/api/internal/store/**`.
- Applies to GORM models, SQLite opener/migration code, repository methods, and tests that exercise persisted product-room data.
- Echo MVP uses SQLite for product room lifecycle persistence; realtime presence, WebSocket connections, speaking state, and reconnect windows stay outside durable storage until a later task explicitly changes that boundary.

### 2. Signatures

- SQLite opener:

```go
func OpenSQLite(path string) (*gorm.DB, error)
```

- Repository constructor:

```go
func NewRepository(db *gorm.DB) *Repository
```

- Create-room persistence method:

```go
func (r *Repository) CreateRoomWithMember(ctx context.Context, room domain.Room, member domain.Member) error
```

- Current durable tables:

```text
rooms(id, name, invite_code, livekit_room_name, host_anonymous_id,
      host_nickname, host_avatar_id, state, created_at,
      last_empty_at, expires_at, updated_at)

members(id, room_id, anonymous_id, nickname, avatar_id, is_host,
        state, muted, speaking, voice_mode, joined_at, livekit_identity)
```

### 3. Contracts

- `OpenSQLite` must reject a blank path before opening the DB.
- Use GORM `AutoMigrate` for MVP bootstrap migrations.
- Use the pure-Go SQLite dialector `github.com/glebarez/sqlite` while repo tests run with `CGO_ENABLED=0`.
- `rooms.invite_code` must have a unique index; room creation code must treat invite-code uniqueness as a business retry signal, not as a generic 500 when it is the only failure.
- `CreateRoomWithMember` must insert room and host member in one transaction.
- Product room fields persist lifecycle facts; host member state persists the initial creator membership. Do not infer the host member later only from room host fields.

### 4. Validation & Error Matrix

| Condition | Required behavior |
| --- | --- |
| SQLite path is blank | Return an error; do not create an in-memory implicit database. |
| DB open fails | Return the original error to the caller. |
| Migration fails after DB open | Close the opened DB pool, then return the original migration error. |
| `rooms.invite_code` unique constraint fails during create | Return `domain.ErrInviteCodeConflict` so the room service can retry generation. |
| Member insert fails after room insert in create transaction | Roll back the room insert and return the error. |
| Initial created room has no members beyond host | Persist exactly the host member for Issue #10; join path is out of scope. |

### 5. Good/Base/Bad Cases

- Good: `OpenSQLite(tempPath)` migrates `rooms` and `members`; `CreateRoomWithMember` persists an `active` room with nil `last_empty_at` / `expires_at` and a host member that is online, unmuted, not speaking, and `push_to_talk`.
- Base: a repository method receives already-validated domain structs and only owns persistence/transaction behavior.
- Bad: storing product room lifecycle in LiveKit, using WebSocket presence as durable state, or creating a room row before separately inserting a host member without transaction rollback.

### 6. Tests Required

- Store migration test:
  - opens a temp-file SQLite database;
  - verifies AutoMigrate allows reading/writing `RoomModel` and `MemberModel`;
  - verifies or explicitly documents the cleanup path when migration fails after the DB pool opens.
- Persistence round-trip test:
  - creates a room + host member through `CreateRoomWithMember`;
  - asserts invite code, active state, nil expiry fields, host flag, online state, unmuted, not speaking, and `push_to_talk`.
- Unique invite test:
  - creates two rooms with the same invite code;
  - asserts the second call returns `domain.ErrInviteCodeConflict`.
- Full backend check:

```bash
go test -count=1 ./services/api/...
```

### 7. Wrong vs Correct

#### Wrong

```go
roomDB := store.RoomModel{InviteCode: code}
if err := db.Create(&roomDB).Error; err != nil {
	return err // caller cannot distinguish retryable invite collision
}
if err := db.Create(&memberDB).Error; err != nil {
	return err // room row may remain without host member
}
```

Why wrong: it leaks DB-specific errors to business logic and can persist a room without its host member.

#### Correct

```go
err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
	if err := tx.Create(roomToModel(room)).Error; err != nil {
		if isInviteCodeConflict(err) {
			return domain.ErrInviteCodeConflict
		}
		return err
	}
	return tx.Create(memberToModel(member)).Error
})
```

Why correct: the transaction keeps room/member persistence atomic and the sentinel error gives the service a precise retry contract.

---

## Scenario: Join-room SQLite repository access

### 1. Scope / Trigger

- Trigger: adding or modifying repository methods used by `POST /v1/rooms/join` or other invite-code join flows.
- Applies to persisted room lookup, member capacity counting, new member insertion, and marking rooms expired when `expires_at <= now`.
- Echo MVP still stores product room lifecycle and current member rows in SQLite; LiveKit must not become the authority for invite validity, expiry, or product capacity.

### 2. Signatures

```go
func (r *Repository) FindRoomByInviteCode(ctx context.Context, inviteCode string) (domain.Room, error)
func (r *Repository) CountRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) (int, error)
func (r *Repository) CreateMember(ctx context.Context, member domain.Member) error
func (r *Repository) MarkRoomExpired(ctx context.Context, roomID string, updatedAt time.Time) error
```

Relevant persisted states:

```go
const (
    MemberStateOnline       MemberState = "online"
    MemberStateReconnecting MemberState = "reconnecting"
    MemberStateDisconnected MemberState = "disconnected"
)
```

### 3. Contracts

- `FindRoomByInviteCode` receives an already-normalized invite code and queries `rooms.invite_code`; it must not normalize or validate presentation input.
- Missing room rows must be translated to `domain.ErrRoomNotFound` at the repository boundary.
- `CountRoomMembersByStates` counts only the caller-provided states; join-room capacity must call it with `online` and `reconnecting`.
- `disconnected` members do not count toward the MVP room capacity limit.
- `CreateMember` inserts exactly one member row and must preserve the service-provided `is_host`, voice state, and `livekit_identity` fields.
- `MarkRoomExpired` updates `rooms.state` to `expired` and refreshes `updated_at`; if no row matches, return `domain.ErrRoomNotFound`.
- Repository methods should accept `context.Context` from the service/handler chain.

### 4. Validation & Error Matrix

| Condition | Required repository behavior |
| --- | --- |
| repository or DB is nil | Return an error; do not panic. |
| invite code has no matching room | Return `domain.ErrRoomNotFound`. |
| `states` argument to count is empty | Return `0, nil`; do not generate invalid SQL. |
| DB count fails | Return the DB error to the service. |
| member insert fails | Return the DB error to the service. |
| expire update matches no room | Return `domain.ErrRoomNotFound`. |
| expire update succeeds | Room state becomes `expired`; `updated_at` is set to the provided timestamp. |

### 5. Good/Base/Bad Cases

- Good: service normalizes invite code, repository finds the room by canonical code, service checks expiry/capacity, then repository inserts a non-host online member.
- Base: repository methods map between GORM models and `domain.Room` / `domain.Member` without applying product copy or HTTP status rules.
- Bad: handler or service depends on raw `gorm.ErrRecordNotFound`, counts every historical member row as active capacity, or asks LiveKit how many product members are allowed.

### 6. Tests Required

- Room lookup test:
  - persisted room can be loaded by invite code;
  - missing invite code returns `domain.ErrRoomNotFound`.
- Member count tests:
  - counts `online` and `reconnecting` members for a room;
  - excludes `disconnected` members;
  - returns zero for empty state list.
- Member insertion test:
  - `CreateMember` persists a non-host joined member with `online`, unmuted, not speaking, `push_to_talk`, and `livekit_identity == member.ID`.
- Expiry update test:
  - `MarkRoomExpired` changes state and `updated_at`;
  - missing room returns `domain.ErrRoomNotFound`.
- Full backend check:

```bash
go test -count=1 ./services/api/...
```

### 7. Wrong vs Correct

#### Wrong

```go
var count int64
db.Model(&store.MemberModel{}).Where("room_id = ?", roomID).Count(&count)
if count >= 10 {
    return room.ErrRoomFull
}
```

Why wrong: it counts disconnected historical members and can reject valid joins.

#### Correct

```go
count, err := repository.CountRoomMembersByStates(ctx, roomID, []domain.MemberState{
    domain.MemberStateOnline,
    domain.MemberStateReconnecting,
})
if err != nil {
    return JoinResult{}, err
}
if count >= maxRoomMembers {
    return JoinResult{}, ErrRoomFull
}
```

Why correct: capacity follows the product invariant: online and reconnecting members occupy slots; disconnected members do not.

---

## Common Mistakes

- Do not switch to `gorm.io/driver/sqlite` without verifying CGO behavior in this repository; current automated checks run with `CGO_ENABLED=0`.
- Do not add durable presence, reconnect, or speaking tables as part of create-room work; those are separate runtime-state concerns.
- Do not depend on raw SQLite error strings outside the store layer; translate invite collisions once at the persistence boundary.
- Do not count `disconnected` member rows toward join-room capacity.
