# Issue 13：房间会话凭证与 LiveKit 短期凭证

## Goal

为已经成功创建或加入临时房间的成员签发短期房间会话凭证和短期 LiveKit 加入凭证，使客户端后续能用业务服务授权连接产品房间状态流，并只允许真实房间成员获取对应 LiveKit 房间的媒体凭证。

## User Value

- 客户端不再只凭本机匿名身份声称自己是房间成员。
- 成员拿到的 LiveKit token 只适用于当前临时房间和当前成员身份。
- token 过期、被篡改或与成员/房间不匹配时，业务服务能拒绝连接凭证获取，降低邀请码泄露或客户端伪造带来的误入风险。

## Confirmed Facts

- GitHub Issue #13 `[S08] 房间会话凭证与 LiveKit 短期凭证` 要求：创建/加入成功后可获得短期房间会话凭证；只有有效房间成员能请求 LiveKit 凭证；过期、篡改、成员不匹配的凭证被拒绝；LiveKit token 的 identity、room、TTL 与设计一致；日志不输出 token 明文。
- Issue #13 blocked by #11；#11 已关闭，当前代码已有创建、加入、离开、SQLite 房间/成员持久化和基本 OpenAPI：`services/api/internal/http/handlers.go`、`services/api/internal/room/service.go`、`services/api/internal/store/sqlite.go`、`services/api/openapi.yaml`。
- `design.md` 区分 `anonymous_id`、`member_id`、`livekit_identity` 和 `room_session_token`，其中 `room_session_token` 用于证明用户已成功加入并做 WebSocket 授权，避免只凭 `anonymous_id` 伪装成员。
- `design.md` 要求创建/加入返回房间快照、member ID、房间会话 token 和 LiveKit token；`POST /v1/rooms/{room_id}/livekit-token` 用于重连或媒体恢复时签发新的 LiveKit token。
- `design.md` 要求 LiveKit token 默认有效期 10 分钟，只允许加入对应 LiveKit room 和 participant identity，签发结果可记录但不得记录 token 明文。
- 已确认 `room_session_token` MVP 默认 TTL 为 2 小时，本 Issue 不实现自动续期。
- `prd.md` 和 `design.md` 都禁止日志记录连接凭证明文、LiveKit token 明文、音频内容或敏感请求体。
- 当前 `services/api/internal/http/handlers.go` 的创建/加入响应只有 `room` 和 `member`，尚未返回 `room_session_token`、`livekit_url` 或 `livekit_token`。
- 当前路由没有 `POST /v1/rooms/{room_id}/livekit-token`；当前仓库没有 `services/api/internal/session` 或 `services/api/internal/livekit` 包。
- Context7 LiveKit docs show Go token issuance through `github.com/livekit/protocol/auth`: `auth.NewAccessToken(apiKey, apiSecret)`, `auth.VideoGrant{RoomJoin: true, Room: roomName}`, `SetIdentity`, `SetValidFor`, and `ToJWT()`.

## Requirements

### R1 — Create/join responses include credentials

`POST /v1/rooms` and `POST /v1/rooms/join` success responses must keep the existing `room` and `member` payloads and add:

- `room_session_token` — business-service signed token for the returned `room.id` and `member.id`.
- `livekit_url` — configured LiveKit server URL.
- `livekit_token` — short-lived LiveKit join token for `room.livekit_room_name` and `member.livekit_identity`.

### R2 — Room session token proves membership and rejects tampering

The room session token must include at least:

- token version or equivalent stable format marker;
- `room_id`;
- `member_id`;
- expiry timestamp.

The service must verify signature, expiry, required claims, and room/member match. Expired, malformed, tampered, wrong-secret, missing-claim, wrong-room, and wrong-member tokens must not authorize LiveKit credential issuance.

### R3 — Credential TTLs are short and explicit

- `room_session_token` default TTL must be 2 hours.
- LiveKit token default TTL must be 10 minutes.
- This Issue must not implement automatic token renewal.
- TTLs must be configurable through API service config fields rather than hard-coded inside handlers.

### R4 — Only valid room members can request a fresh LiveKit token

`POST /v1/rooms/{room_id}/livekit-token` must accept a room session token, verify it, then verify the referenced member still belongs to the requested room and is an active member state (`online` or `reconnecting`). Missing members, disconnected members, expired rooms, and room/member mismatches must be rejected.

### R5 — LiveKit token scope is minimal and design-aligned

The LiveKit token must:

- use the configured LiveKit API key and secret;
- use the product room's `LiveKitRoomName` as the LiveKit `room` grant;
- use the member's `LiveKitIdentity` as the participant identity;
- use the member nickname as display name when available;
- have a default TTL of 10 minutes;
- grant only room join plus publish/subscribe permissions needed for voice participants;
- not grant admin, room-create management, SIP, agent dispatch, or unrelated permissions.

### R6 — HTTP errors use the existing JSON error envelope

All new HTTP failures must use the existing shape:

```json
{
  "error": {
    "code": "...",
    "message": "..."
  }
}
```

Expected public errors must include invalid/missing session token, expired session token, room/member mismatch, room not found, member not found or inactive, expired room, and token service configuration failure mapped without exposing secrets.

### R7 — OpenAPI documents credential fields and endpoint

`services/api/openapi.yaml` must document:

- added credential fields on create/join success responses;
- `POST /v1/rooms/{room_id}/livekit-token` request authorization expectation;
- success response containing `livekit_url` and `livekit_token`;
- every public error code returned by the new credential path.

### R8 — No token persistence or token plaintext logging

Room session tokens and LiveKit tokens must not be stored in SQLite and must not be logged. Logs may record success/failure categories and non-secret IDs (`room_id`, `member_id`) if logging exists, but must not include token strings, API secrets, room session secrets, request bodies, or audio data.

### R9 — Configuration stays explicit

The credential path must use existing config concepts (`LiveKitURL`, `LiveKitAPIKey`, `LiveKitAPISecret`, `RoomSessionSecret`) plus explicit TTL config values. Blank required credential config must fail token issuance clearly without falling back to insecure hard-coded production secrets.

## Acceptance Criteria

- [ ] AC1: `POST /v1/rooms` returns `room`, `member`, `room_session_token`, `livekit_url`, and `livekit_token`; existing create-room behavior remains intact.
- [ ] AC2: `POST /v1/rooms/join` returns `room`, `member`, `room_session_token`, `livekit_url`, and `livekit_token`; existing join validation, expiry, capacity, and duplicate-nickname behavior remains intact.
- [ ] AC3: Session-token unit tests cover sign/verify success, 2-hour default TTL usage, expiry rejection, tampering rejection, wrong-secret rejection, malformed token rejection, and missing required claims.
- [ ] AC4: LiveKit-token unit tests verify identity, room grant, 10-minute default TTL usage, expiry metadata, and no extra admin/SIP/agent grants.
- [ ] AC5: Fresh LiveKit-token HTTP tests prove a valid active member can obtain a token for its room, while expired/tampered/wrong-room/wrong-member/disconnected-member credentials are rejected.
- [ ] AC6: Store/room-service tests verify member authorization uses product-room state from SQLite, not LiveKit participant presence or anonymous identity alone.
- [ ] AC7: OpenAPI matches implemented request/response fields and error codes.
- [ ] AC8: `go test -count=1 ./services/api/...` passes and `git diff --check` passes.
- [ ] AC9: Review confirms no token plaintext, API secret, room session secret, sensitive request body, or audio data is logged or persisted.

## Out of Scope

- WebSocket runtime implementation.
- Automatic room-session-token renewal.
- Long-lived tokens or persistent token storage.
- Accounts, login, friends, fixed rooms, room passwords, 房主管理, or token revocation controls.
- Using LiveKit as product-room membership, capacity, lifecycle, or invite-code authority.
- TURN, deployment files, desktop client LiveKit connection code, or frontend UI changes.

## Open Questions

None.
