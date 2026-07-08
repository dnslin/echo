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

- Invalid JSON or binding failure returns HTTP `400` with code `invalid_request` and message `请求格式无效`.
- Service validation errors return HTTP `400` and preserve the service-provided `Code` and `Message` exactly.
- Unexpected service/store failures return HTTP `500` with code `internal_error` and message `服务器错误`.
- `POST /v1/rooms` success returns HTTP `201` and must not include LiveKit token, room session token, join-room result, or WebSocket data.
- OpenAPI must document every public error code exposed by the endpoint.

### 4. Validation & Error Matrix

| Condition | HTTP status | Error code | Message |
| --- | --- | --- | --- |
| Malformed JSON | `400` | `invalid_request` | `请求格式无效` |
| `anonymous_id` blank after trim | `400` | `invalid_anonymous_id` | `匿名身份不能为空` |
| `avatar_id` blank after trim | `400` | `invalid_avatar_id` | `请选择头像` |
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
  - empty `avatar_id`;
  - empty `nickname`;
  - nickname over 16 runes;
  - room name over 24 runes;
  - each asserts status, error code, and exact Chinese message.
- Contract check:
  - `services/api/openapi.yaml` documents the endpoint, request body, success response, and 400 error examples.

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

## Common Mistakes

- Do not spread request validation across handler, service, and store; validate product inputs at the service boundary and translate once in HTTP.
- Do not change user-facing Chinese copy without checking `prd.md`.
- Do not document an error code in OpenAPI unless the handler can actually return it, and do not return a code from code unless OpenAPI documents it.
