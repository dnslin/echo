# Issue 10 创建临时房间 API Design

## First-Principles Analysis

### Challenge assumptions

- 未验证假设：创建房间必须立即接入 LiveKit token。Issue #10 明确排除 LiveKit token，因此本任务不做。
- 未验证假设：必须先实现完整 room lifecycle。创建路径只需要初始有效状态；30 分钟空房、加入、重连属于后续路径。
- 类比性假设：需要账号或用户表。MVP 的原子事实是本地匿名身份足以标识本机用户，不能引入账号语义。
- 潜在错误假设：可以把产品房间交给 LiveKit room 表达。ADR 已确认 LiveKit 只拥有媒体房间，产品房间必须由业务服务持久化。

### Bedrock truths

- HTTP 创建命令的可观察事实只有请求、响应、状态码和持久化状态。
- 6 位邀请码是有限字符集合上的服务端生成值，数据库唯一约束是并发下保护唯一性的最小可靠机制。
- 房主只是创建者成员的布尔标识，不授予管理能力。
- SQLite 是本任务唯一需要的持久化资源；实时连接和媒体不参与创建路径。
- 用户能看到的错误必须来自产品文案：昵称为空、昵称过长、房间名过长。

### Rebuild from truths

1. 接收创建请求并归一化文本字段。
2. 用小型 validator 拦截无效 nickname / room_name，返回稳定错误码和中文消息。
3. 在业务服务中生成 room ID、member ID、livekit room name 和 6 位邀请码。
4. 用 GORM 在事务中插入 room 与 host member；invite code 设置唯一索引，冲突时重试。
5. HTTP handler 把 domain 结果映射为创建响应；不生成 token，不触碰 WebSocket。
6. OpenAPI 与 handler 测试共同锁定契约。

### Contrast with convention

常规 CRUD 思路可能先铺完整房间、成员、加入、token、WebSocket 和 LiveKit 集成；这里会扩大未验证面。基于本任务的基本事实，最小完整垂直切片是“校验输入 → 持久化 active room + host member → 返回 invite code 和快照”。这不是最小化实现，而是严格满足当前 Issue 的完整边界。

### Conclusion

本任务应实现一个持久化的业务创建路径，而不是媒体或实时状态路径。完成后，后续加入房间、token 和 WebSocket 可以复用同一 room/member 模型。

## Architecture Boundaries

- `services/api/internal/http`：Gin router、request/response structs、HTTP 状态码与 JSON 错误映射。
- `services/api/internal/room`：创建临时房间的业务规则、输入校验、邀请码冲突重试、domain result。
- `services/api/internal/invite`：6 位邀请码生成与字符规则。
- `services/api/internal/store`：GORM models、SQLite open/migration、room/member persistence。
- `services/api/internal/domain`：房间与成员状态枚举、domain structs。
- `services/api/internal/config`：创建路径需要的配置默认值，如邀请码长度。
- `services/api/openapi.yaml`：HTTP 契约来源。

No imports may cross into `apps/desktop/internal/*`.

## Data Model

### rooms

- `id` string primary key, e.g. `room_<hex>`
- `name` string, max 24, not null
- `invite_code` string, size 6, unique index
- `livekit_room_name` string, not null
- `host_anonymous_id` string, not null
- `host_nickname` string, max 16, not null
- `host_avatar_id` string, not null
- `state` string: `active | expired`
- `created_at` time, not null
- `last_empty_at` nullable time
- `expires_at` nullable time
- `updated_at` time

### members

Although Issue #10 does not implement join/reconnect, the response must return the host member. Persisting the host member now prevents the later join path from needing to infer membership from duplicated room host fields.

- `id` string primary key, e.g. `mem_<hex>`
- `room_id` indexed foreign-like reference to rooms
- `anonymous_id` string, not null
- `nickname` string, max 16, not null
- `avatar_id` string, not null
- `is_host` bool, true for creator
- `state` string: initial `online`
- `muted` bool: initial false
- `speaking` bool: initial false
- `voice_mode` string: initial `push_to_talk`
- `joined_at` time, not null
- `livekit_identity` string, equal to member id for now

## API Contract

### Request

`POST /v1/rooms`

```json
{
  "anonymous_id": "anon_local_123",
  "nickname": "Alice",
  "avatar_id": "avatar_07",
  "room_name": "今晚开黑"
}
```

### Success response: 201

```json
{
  "room": {
    "id": "room_abc",
    "name": "今晚开黑",
    "invite_code": "K7M9Q2",
    "state": "active",
    "created_at": "2026-07-08T12:00:00Z",
    "last_empty_at": null,
    "expires_at": null
  },
  "member": {
    "id": "mem_abc",
    "room_id": "room_abc",
    "anonymous_id": "anon_local_123",
    "nickname": "Alice",
    "avatar_id": "avatar_07",
    "is_host": true,
    "state": "online",
    "muted": false,
    "speaking": false,
    "voice_mode": "push_to_talk",
    "livekit_identity": "mem_abc",
    "joined_at": "2026-07-08T12:00:00Z"
  }
}
```

Top-level duplicate `invite_code` is not required if `room.invite_code` is present. If existing frontend expectation later needs a flattened field, add it in a later compatible change.

### Error response: 400

```json
{
  "error": {
    "code": "invalid_nickname",
    "message": "请输入昵称"
  }
}
```

Error code suggestions:

- `invalid_anonymous_id`
- `invalid_avatar_id`
- `invalid_nickname`
- `nickname_too_long`
- `room_name_too_long`

## Compatibility and Migration

- Existing `/healthz` behavior must remain unchanged.
- SQLite AutoMigrate can create new tables on startup; no existing production data is assumed.
- `go.work` and module path remain unchanged.
- OpenAPI expands from health-only to include create room; WebSocket docs are not modified for this issue.

## Risks and Mitigations

- Invite collision: use database unique index plus bounded retry.
- Unicode length ambiguity: count runes for nickname and room name because product copy speaks to user-visible characters, not bytes.
- Over-scoping: explicitly avoid token, join, WebSocket and lifecycle expiry beyond initial fields.
- Persistence drift: handler tests should assert response shape and service/store tests should assert persisted room/member fields.

## Verification

- `go test ./services/api/internal/invite -v`
- `go test ./services/api/internal/store -v`
- `go test ./services/api/internal/room -v`
- `go test ./services/api/internal/http -v`
- `go test ./services/api/...`
