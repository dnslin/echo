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
| DB open or migration fails | Return the original error to the caller. |
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
  - verifies AutoMigrate allows reading/writing `RoomModel` and `MemberModel`.
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

## Common Mistakes

- Do not switch to `gorm.io/driver/sqlite` without verifying CGO behavior in this repository; current automated checks run with `CGO_ENABLED=0`.
- Do not add durable presence, reconnect, or speaking tables as part of create-room work; those are separate runtime-state concerns.
- Do not depend on raw SQLite error strings outside the store layer; translate invite collisions once at the persistence boundary.
