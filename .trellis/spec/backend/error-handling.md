# Error Handling

> Error handling contracts for the echo API service.

---

## Scenario: Create temporary room HTTP validation errors

### 1. Scope / Trigger

- Trigger: adding or modifying `POST /v1/rooms` and other Gin HTTP command endpoints under `services/api/internal/http/**`.
- Applies to request binding, service validation errors, HTTP status mapping, JSON error envelopes, and OpenAPI error examples.
- Product-facing validation messages must match `prd.md`; handlers should not invent alternate Chinese copy for established product errors.

### 2. Signatures

- Create-room endpoint:

```http
POST /v1/rooms
Content-Type: application/json
```

- Create-room request body:

```json
{
  "anonymous_id": "anon_local_123",
  "nickname": "Alice",
  "avatar_id": "avatar_07",
  "room_name": "今晚开黑"
}
```

- Service validation error type:

```go
type ValidationError struct {
	Code    string
	Message string
}
```

- HTTP error envelope:

```json
{
  "error": {
    "code": "invalid_nickname",
    "message": "请输入昵称"
  }
}
```

### 3. Contracts

- Invalid JSON, oversized request bodies, or binding failure returns HTTP `400` with code `invalid_request` and message `请求格式无效`.
- `POST /v1/rooms` must cap request-body bytes before JSON binding so oversized input is rejected before service creation starts.
- Service validation errors return HTTP `400` and preserve the service-provided `Code` and `Message` exactly.
- Unexpected service/store failures return HTTP `500` with code `internal_error` and message `服务器错误`.
- `POST /v1/rooms` success returns HTTP `201` and must not include LiveKit token, room session token, join-room result, or WebSocket data.
- OpenAPI must document every public error code exposed by the endpoint, including `internal_error` in a `500` response.

### 4. Validation & Error Matrix

| Condition | HTTP status | Error code | Message |
| --- | --- | --- | --- |
| Malformed or oversized JSON | `400` | `invalid_request` | `请求格式无效` |
| `anonymous_id` blank after trim | `400` | `invalid_anonymous_id` | `匿名身份不能为空` |
| `anonymous_id` longer than 128 runes after trim | `400` | `anonymous_id_too_long` | `匿名身份最多 128 个字符` |
| `avatar_id` blank after trim | `400` | `invalid_avatar_id` | `请选择头像` |
| `avatar_id` longer than 64 runes after trim | `400` | `avatar_id_too_long` | `头像标识最多 64 个字符` |
| `nickname` blank after trim | `400` | `invalid_nickname` | `请输入昵称` |
| `nickname` longer than 16 runes | `400` | `nickname_too_long` | `昵称最多 16 个字符` |
| `room_name` longer than 24 runes | `400` | `room_name_too_long` | `房间名称最多 24 个字符` |
| Non-validation service/store failure | `500` | `internal_error` | `服务器错误` |

### 5. Good/Base/Bad Cases

- Good: handler binds JSON, calls one service method, maps `room.ValidationError` to the standard error envelope, and delegates copy/validation rules to the service.
- Base: `/healthz` remains available without constructing product service dependencies.
- Bad: handler duplicates nickname length logic, returns plain text errors, or exposes raw DB errors to clients.

### 6. Tests Required

- Router smoke test:
  - `GET /healthz` returns `200` and `{ "status": "ok" }`.
- Create-room success HTTP test:
  - `POST /v1/rooms` returns `201`;
  - response contains `room.invite_code` matching `^[A-Z0-9]{6}$`;
  - `member.is_host` is true;
  - room is `active` with nil `last_empty_at` and `expires_at`.
- Validation HTTP tests:
  - empty `anonymous_id`;
  - `anonymous_id` over 128 runes;
  - empty `avatar_id`;
  - `avatar_id` over 64 runes;
  - empty `nickname`;
  - nickname over 16 runes;
  - room name over 24 runes;
  - oversized request body returns `invalid_request` before room creation starts;
  - each asserts status, error code, and exact Chinese message.
- Context propagation test:
  - handler passes `c.Request.Context()` to the room creation service.
- Contract check:
  - `services/api/openapi.yaml` documents the endpoint, request body, success response, 400 error examples, and `500 internal_error` response.

### 7. Wrong vs Correct

#### Wrong

```go
if request.Nickname == "" {
	c.String(http.StatusBadRequest, "bad nickname")
	return
}
```

Why wrong: it duplicates service validation, bypasses the JSON error envelope, and loses product-approved Chinese copy.

#### Correct

```go
result, err := h.roomCreator.Create(input)
if err != nil {
	var validationErr *room.ValidationError
	if errors.As(err, &validationErr) {
		writeError(c, http.StatusBadRequest, validationErr.Code, validationErr.Message)
		return
	}
	writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
	return
}
```

Why correct: validation remains owned by the service, and the handler owns only HTTP status/envelope mapping.

---

## Scenario: Join temporary room HTTP validation errors

### 1. Scope / Trigger

- Trigger: adding or modifying `POST /v1/rooms/join` or other invite-code join command endpoints under `services/api/internal/http/**`.
- Applies to request binding, invite validation mapping, room service product errors, HTTP status mapping, JSON error envelopes, and OpenAPI error examples.
- Product-facing validation messages must match `prd.md`; handlers should not invent alternate Chinese copy for established join failures.

### 2. Signatures

- Join-room endpoint:

```http
POST /v1/rooms/join
Content-Type: application/json
```

- Join-room request body:

```json
{
  "invite_code": "k7-m9 q2",
  "anonymous_id": "anon_local_456",
  "nickname": "Alice",
  "avatar_id": "avatar_08"
}
```

- Room service call:

```go
func (s *Service) JoinContext(ctx context.Context, input room.JoinInput) (room.JoinResult, error)
```

- Success response shape reuses the create-room room/member envelope:

```json
{
  "room": {
    "id": "room_abc",
    "invite_code": "K7M9Q2",
    "state": "active"
  },
  "member": {
    "id": "mem_joined",
    "room_id": "room_abc",
    "is_host": false,
    "state": "online",
    "muted": false,
    "speaking": false,
    "voice_mode": "push_to_talk"
  }
}
```

### 3. Contracts

- `POST /v1/rooms/join` must cap request-body bytes before JSON binding, using the same request-size limit as create-room unless a later spec changes both endpoints.
- Invalid JSON, oversized request bodies, or binding failure returns HTTP `400` with code `invalid_request` and message `请求格式无效`.
- Invite-code format errors are service validation errors, not store errors.
- Join success returns HTTP `200` and must not include LiveKit token, room session token, WebSocket data, room-owner controls, or account information.
- Duplicate nicknames in the same room are allowed and must not be rejected by the HTTP layer.
- OpenAPI must document every public join error code the handler can return.

### 4. Validation & Error Matrix

| Condition | HTTP status | Error code | Message |
| --- | --- | --- | --- |
| Malformed or oversized JSON | `400` | `invalid_request` | `请求格式无效` |
| `invite_code` empty after normalization ignores whitespace / hyphens | `400` | `empty_invite_code` | `请输入邀请码` |
| normalized invite code is not 6 `A-Z0-9` characters | `400` | `invalid_invite_format` | `邀请码应为 6 位字母或数字` |
| `anonymous_id` blank after trim | `400` | `invalid_anonymous_id` | `匿名身份不能为空` |
| `anonymous_id` longer than 128 runes after trim | `400` | `anonymous_id_too_long` | `匿名身份最多 128 个字符` |
| `avatar_id` blank after trim | `400` | `invalid_avatar_id` | `请选择头像` |
| `avatar_id` longer than 64 runes after trim | `400` | `avatar_id_too_long` | `头像标识最多 64 个字符` |
| `nickname` blank after trim | `400` | `invalid_nickname` | `请输入昵称` |
| `nickname` longer than 16 runes | `400` | `nickname_too_long` | `昵称最多 16 个字符` |
| normalized invite code has no room | `404` | `invite_not_found` | `邀请码无效，请检查后重试` |
| room is expired or `expires_at <= now` | `410` | `room_expired` | `该房间已过期，请让朋友重新创建` |
| room already has 10 online/reconnecting members | `409` | `room_full` | `房间人数已满，暂时无法加入` |
| Non-validation service/store failure | `500` | `internal_error` | `服务器错误` |

### 5. Good/Base/Bad Cases

- Good: handler binds JSON, passes `c.Request.Context()` to `JoinContext`, maps room service sentinels to the standard error envelope, and reuses the room/member response projection.
- Base: create-room and join-room share identity/display validation messages, but only join-room validates `invite_code`.
- Bad: handler queries `rooms` directly, compares raw invite input, rejects duplicate nicknames, or returns plain text errors.

### 6. Tests Required

- Join-room success HTTP test:
  - create or seed a room;
  - call `POST /v1/rooms/join` with lower-case / spaced / hyphenated invite input;
  - assert HTTP `200`, matching room ID, normalized invite code, `member.is_host == false`, `online`, unmuted, not speaking, and `push_to_talk`.
- Validation HTTP tests:
  - empty invite;
  - invalid invite length;
  - invalid invite character;
  - empty / overlong anonymous ID;
  - empty / overlong avatar ID;
  - empty / overlong nickname;
  - malformed or oversized request body does not call the service.
- Product error HTTP tests:
  - unknown invite returns `404 invite_not_found`;
  - expired room returns `410 room_expired`;
  - full room returns `409 room_full`;
  - duplicate nickname succeeds.
- Contract check:
  - `services/api/openapi.yaml` documents `POST /v1/rooms/join`, request body, success response, 400 / 404 / 409 / 410 / 500 error examples, and every returned error code.

### 7. Wrong vs Correct

#### Wrong

```go
func (h *Handlers) JoinRoom(c *gin.Context) {
    if request.InviteCode == "" {
        c.String(http.StatusBadRequest, "bad invite")
        return
    }
    // direct DB lookup from handler...
}
```

Why wrong: it duplicates product validation, bypasses the JSON error envelope, and couples HTTP to persistence details.

#### Correct

```go
result, err := h.roomJoiner.JoinContext(c.Request.Context(), room.JoinInput{
    InviteCode: request.InviteCode,
    AnonymousID: request.AnonymousID,
    Nickname: request.Nickname,
    AvatarID: request.AvatarID,
})
if err != nil {
    writeRoomError(c, err)
    return
}
```

Why correct: the service owns product rules, and HTTP owns only transport binding plus status/envelope mapping.

---

## Common Mistakes

- Do not spread request validation across handler, service, and store; validate product inputs at the service boundary and translate once in HTTP.
- Do not change user-facing Chinese copy without checking `prd.md`.
- Do not document an error code in OpenAPI unless the handler can actually return it, and do not return a code from code unless OpenAPI documents it.
- Do not reject duplicate nicknames in join-room handling; nickname uniqueness is not an MVP invariant.
