# 修复 PR32 code review findings

## Goal

修复 PR #32 code-review 确认的 create-room 后端问题，使 `POST /v1/rooms` 的实现、OpenAPI 契约、资源边界和请求取消语义一致。

用户价值：创建临时房间 API 在异常、取消和恶意大输入场景下行为可预期，不让客户端契约漂移，也不把无效大数据写入 SQLite。

## Confirmed Findings

- F1：`services/api/internal/http/handlers.go` 会返回 `500 internal_error`，但 `services/api/openapi.yaml` 未声明 500 响应且 `ErrorResponse.code` enum 缺少 `internal_error`。
- F2：handler 调用 context-free `Create`，未把 HTTP request context 传给 service/store。
- F3：handler 在 JSON 解码前没有 request body size cap。
- F4：`anonymous_id` 与 `avatar_id` 只校验非空，可能超过持久化字段预期大小并被回显。
- F5：`OpenSQLite` 在 `AutoMigrate` 失败时未关闭已经打开的 DB pool。

## Requirements

### R1 OpenAPI error contract

- `POST /v1/rooms` must document server failures with HTTP `500` and the standard error envelope.
- `ErrorResponse.code` must include every public code the handler can return, including `internal_error`.
- The contract should keep validation failures as `400` and success as `201`.

### R2 Request cancellation

- The create-room HTTP handler must call a context-aware room creation method.
- The request context from `c.Request.Context()` must reach the room service and repository transaction.
- Keep `/healthz` behavior unchanged.

### R3 Request and field size bounds

- `POST /v1/rooms` must reject oversized request bodies before unbounded JSON allocation.
- `anonymous_id` and `avatar_id` must be trimmed and bounded consistently with persistence field sizes.
- New validation errors must return `400` using the same JSON error envelope and be documented in OpenAPI.

### R4 SQLite migration failure cleanup

- If SQLite opens but `AutoMigrate` fails, `OpenSQLite` must close the underlying DB pool before returning the migration error.

## Acceptance Criteria

- [ ] AC1：OpenAPI documents `POST /v1/rooms` `500 internal_error` and `ErrorResponse.code` includes `internal_error`.
- [ ] AC2：A handler regression test proves `CreateRoom` passes the HTTP request context to room creation.
- [ ] AC3：An oversized create-room JSON request returns `400 invalid_request` without reaching room creation.
- [ ] AC4：Service and HTTP tests cover overlong `anonymous_id` and `avatar_id`.
- [ ] AC5：`OpenSQLite` closes the DB pool on migration failure; if no black-box repro seam is available, the cleanup branch is implemented directly and noted.
- [ ] AC6：Existing create-room success and validation behavior remains passing.
- [ ] AC7：`go test -count=1 ./services/api/...` passes.
- [ ] AC8：`git diff --check` passes or only reports non-blocking Windows CRLF conversion warnings.

## Out of Scope

- Join-room, LiveKit token, WebSocket state stream, room session token, reconnect, expiry runtime, account/fixed-room/history behavior.
- Posting GitHub review comments or merging PR #32.
