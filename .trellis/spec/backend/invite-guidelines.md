# Invite Code Guidelines

> Invite-code generation and normalization contracts for the echo API service.

---

## Scenario: Invite code generation and join-input normalization

### 1. Scope / Trigger

- Trigger: adding or modifying invite-code behavior under `services/api/internal/invite/**` or any API flow that accepts user-entered invite codes.
- Applies to generated invite-code alphabet, code length, user input normalization, and tests that prove different visible inputs map to the same temporary room.
- Invite codes are temporary room locators, not passwords or account credentials.

### 2. Signatures

```go
const CharSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var (
    ErrInvalidLength = errors.New("invite code length must be positive")
    ErrEmptyCode     = errors.New("invite code is empty")
    ErrInvalidFormat = errors.New("invite code format is invalid")
)

func Normalize(input string) (string, error)
func (g Generator) Generate(length int) (string, error)
```

### 3. Contracts

- Generated invite codes must use only `A-Z0-9`.
- Generated MVP invite codes are 6 characters long when called by the room service.
- User-entered invite codes must be normalized before lookup:
  - ignore Unicode whitespace;
  - ignore ASCII short hyphens (`-`);
  - convert ASCII `a-z` to `A-Z`;
  - accept only `A-Z0-9` after normalization;
  - require exactly 6 normalized characters.
- `Normalize` must not query the database or decide HTTP status. It only returns a canonical code or a typed invite-package error.

### 4. Validation & Error Matrix

| Condition | Required package behavior | HTTP/product owner |
| --- | --- | --- |
| input contains only ignored whitespace / `-` | return `ErrEmptyCode` | room service maps to `empty_invite_code` / `请输入邀请码` |
| normalized length is not 6 | return `ErrInvalidFormat` | room service maps to `invalid_invite_format` / `邀请码应为 6 位字母或数字` |
| input contains a rune outside `A-Z`, `a-z`, `0-9`, whitespace, `-` | return `ErrInvalidFormat` | room service maps to `invalid_invite_format` / `邀请码应为 6 位字母或数字` |
| input ` k7-m9 q2 ` | return `K7M9Q2`, nil | join service may look up the canonical code |
| `Generate(length <= 0)` | return `ErrInvalidLength` | caller treats generator configuration as service/internal failure |

### 5. Good/Base/Bad Cases

- Good: `Normalize(" k7-m9 q2 ")` returns `K7M9Q2`, and the join-room service performs the room lookup with that canonical value.
- Base: `Generate(6)` returns a 6-byte ASCII string where every byte is in `CharSet`.
- Bad: comparing raw user input directly against `rooms.invite_code`, accepting punctuation, or treating an invite code as a reusable password.

### 6. Tests Required

- Generation tests:
  - `Generate(6)` returns length 6;
  - every generated character is in `CharSet`;
  - invalid length returns an error.
- Normalization tests:
  - mixed case, spaces, and hyphenated input map to the same canonical code;
  - whitespace-only / hyphen-only input returns `ErrEmptyCode`;
  - wrong length returns `ErrInvalidFormat`;
  - invalid characters return `ErrInvalidFormat`.
- Integration tests:
  - `POST /v1/rooms/join` accepts a normalized variant of a persisted/generated invite code.

### 7. Wrong vs Correct

#### Wrong

```go
var room store.RoomModel
err := db.First(&room, "invite_code = ?", request.InviteCode).Error
```

Why wrong: it lets presentation differences such as lower-case letters or spaces determine room lookup results.

#### Correct

```go
code, err := invite.Normalize(input.InviteCode)
if err != nil {
    return JoinResult{}, validationErrorForInvite(err)
}
room, err := repository.FindRoomByInviteCode(ctx, code)
```

Why correct: one canonical code reaches persistence, and product-facing error mapping stays at the service boundary.

---

## Common Mistakes

- Do not use Unicode uppercasing to admit non-ASCII letters; invite codes are generated from the ASCII `A-Z0-9` alphabet only.
- Do not make nickname uniqueness, account identity, or room-owner permissions part of invite-code validation.
- Do not log invite-code input as long-lived history; use request summaries and product error categories instead.
