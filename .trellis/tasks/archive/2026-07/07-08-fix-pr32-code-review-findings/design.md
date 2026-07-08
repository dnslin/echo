# 修复 PR32 code review findings Design

## First-Principles Analysis

### Challenge assumptions

- Unverified assumption: a handler can ignore request cancellation because create-room is short. SQLite can block on file locks or disk I/O, so cancellation is a real resource boundary.
- Unverified assumption: OpenAPI only needs happy-path and validation errors. The handler publicly emits `internal_error`; generated clients need that contract.
- Unverified assumption: DB `size` tags enforce string limits in SQLite. SQLite stores text dynamically unless application validation enforces bounds.
- Potentially wrong assumption: request body limits can wait for gateway/Nginx. The API server is the last authoritative trust boundary and must reject excessive local inputs.

### Bedrock truths

- HTTP request context is the only cancellation signal the server receives for a client-disconnected request.
- JSON decoding allocates data before service validation sees field values.
- SQLite/GORM model tags describe intended sizes but do not guarantee SQLite rejects longer text.
- OpenAPI is the public machine contract; every emitted public error code must appear there.
- A successfully opened DB pool owns OS/file resources until closed.

### Rebuild from truths

1. Add a request-body limit at the handler boundary before `ShouldBindJSON`.
2. Make the handler dependency context-aware and call `CreateContext(c.Request.Context(), input)`.
3. Keep validation ownership in `room.Service`, adding bounded checks for `anonymous_id` and `avatar_id` there.
4. Update OpenAPI for all public validation and server error codes.
5. On `AutoMigrate` failure, close the already opened SQL pool before returning the original migration error.
6. Lock behavior with regression tests at handler and service seams.

### Contrast with convention

A conventional quick patch might only update OpenAPI or only add body size limits. That would leave cancellation, persistence-bound field sizes, and resource cleanup unfixed. The fundamental difference here is treating every input/resource boundary as an explicit contract.

### Conclusion

The correct fix is a small set of boundary hardening changes: context propagation, bounded input, complete OpenAPI error contract, and DB cleanup on migration failure, without adding new product capabilities.

## Design Details

### HTTP layer

- Add `maxCreateRoomRequestBytes` constant.
- Wrap `c.Request.Body` with `http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRoomRequestBytes)` before binding.
- Change the local dependency interface to require:

```go
CreateContext(ctx context.Context, input room.CreateInput) (room.CreateResult, error)
```

- Map oversized JSON body through existing `invalid_request` response.

### Room service

- Add service-owned validation limits:
  - `anonymous_id`: max 128 runes, matching persistence size.
  - `avatar_id`: max 64 runes, matching persistence size.
- New errors:
  - `anonymous_id_too_long`: `匿名身份最多 128 个字符`
  - `avatar_id_too_long`: `头像标识最多 64 个字符`

### Store

- After successful `gorm.Open`, if `AutoMigrate` fails, call `db.DB()` and close the returned `*sql.DB` when available.
- Return the original migration error.

### OpenAPI

- Add 400 examples for the new field-length validation errors and malformed request if absent.
- Add 500 response example for `internal_error`.
- Add `internal_error`, `anonymous_id_too_long`, and `avatar_id_too_long` to `ErrorResponse.code` enum.

## Verification

- Add handler tests for request context propagation and oversized body rejection.
- Add service/HTTP tests for overlong `anonymous_id` and `avatar_id`.
- Run `go test -count=1 ./services/api/...`.
- Run `git diff --check`.
