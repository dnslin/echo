# Issue 10 创建临时房间 API

## Goal

实现 GitHub Issue #10 的后端垂直路径：客户端提交本地匿名身份、昵称、随机头像和可选房间名后，业务服务创建一个有效的 MVP 临时房间，生成 6 位邀请码，并返回房间与房主成员信息。

用户价值：朋友开黑前，房主可以先创建临时房间并拿到邀请码；LiveKit 媒体、加入房间、WebSocket 状态流后续再接入，但产品房间状态必须先由业务服务拥有。

## Confirmed Facts

- GitHub Issue #10 是 `[S05] 创建临时房间 API 垂直路径`，属于 #3，前置 Issue #6/#7/#8/#9 均已关闭。
- 当前后端只有 API 骨架：`services/api/internal/http/router.go:9` 创建 Gin router，`services/api/internal/http/router.go:12` 只注册 `/healthz`。
- 当前 OpenAPI 只记录健康检查：`services/api/openapi.yaml:6`。
- API module 已存在并独立：`services/api/go.mod:1`；root workspace 引用 `./services/api`：`go.work:3`。
- 产品需求要求创建临时房间并获得邀请码：`prd.md:251`；昵称和房间名错误文案分别为 `请输入昵称`、`昵称最多 16 个字符`、`房间名称最多 24 个字符`：`prd.md:812`。
- 邀请码规则为 6 位大写字母数字，输入归一化属于加入流程但生成必须由服务端完成：`prd.md:166`、`docs/adr/0013-server-generated-invite-codes.md:1`。
- 业务服务而非 LiveKit 拥有产品房间、房主标识、邀请码和生命周期：`docs/adr/0008-business-rooms-own-product-state.md:1`。
- HTTP 命令与 WebSocket 状态流分离；本任务只实现 HTTP 创建命令并更新 OpenAPI：`docs/adr/0019-http-commands-websocket-state.md:1`、`docs/adr/0032-openapi-http-websocket-contract-docs.md:1`。

## Requirements

### R1 创建临时房间请求

- 新增 `POST /v1/rooms`。
- 请求 JSON 必须包含：
  - `anonymous_id`：本机匿名身份，非账号。
  - `nickname`：成员展示昵称。
  - `avatar_id`：随机头像标识。
  - `room_name`：可选房间名。
- `nickname` 前后空白归一化后不能为空，最大 16 个字符。
- `room_name` 前后空白归一化后可为空，最大 24 个字符；为空时服务端使用短默认名称，不引入固定房间语义。
- `anonymous_id` 和 `avatar_id` 必须非空；失败返回明确校验错误。

### R2 创建成功业务结果

- 成功创建一个 `active` 临时房间。
- 服务端生成 6 位 `A-Z0-9` 邀请码。
- 创建者成员被标记为房主。
- 房间初始状态有效：`state = active`，`created_at` 已设置，`last_empty_at` 和 `expires_at` 为空。
- 初始成员状态在线，默认未静音、未说话、语音模式为 `push_to_talk`。
- LiveKit room name 可以作为产品房间的派生字段返回或持久化，但本任务不签发 LiveKit token。

### R3 持久化与边界

- 使用 SQLite + GORM 持久化产品房间生命周期数据。
- 邀请码唯一性由数据库约束保护；创建时发生邀请码冲突必须重试，重试耗尽返回服务端错误。
- 本任务只持久化创建临时房间所需数据；实时 presence、WebSocket 连接、speaking 事件和重连窗口不在本任务内实现。
- 不添加账号、固定房间、房间历史、加入房间、LiveKit token、WebSocket 广播、房主管理或服务端音频逻辑。

### R4 HTTP 错误与契约

- 校验失败返回 `400` 和机器可读错误码，以及用户可理解的中文错误消息。
- 创建成功返回 `201`。
- OpenAPI 必须包含 `POST /v1/rooms` 请求体、成功响应和校验失败响应 schema。
- 不在日志、错误或响应中泄漏敏感 token；本任务不生成 token。

## Acceptance Criteria

- [ ] AC1：`POST /v1/rooms` 使用合法 `anonymous_id`、`nickname`、`avatar_id` 和可选 `room_name` 时返回 `201`。
- [ ] AC2：成功响应包含 6 位邀请码，字符只来自 `A-Z0-9`。
- [ ] AC3：成功响应中的创建者成员 `is_host = true`。
- [ ] AC4：成功响应中的房间 `state = active`，并包含创建时间；`last_empty_at` / `expires_at` 不应在初始创建时设置。
- [ ] AC5：昵称为空返回 `400`，错误消息为 `请输入昵称`。
- [ ] AC6：昵称超过 16 个字符返回 `400`，错误消息为 `昵称最多 16 个字符`。
- [ ] AC7：房间名超过 24 个字符返回 `400`，错误消息为 `房间名称最多 24 个字符`。
- [ ] AC8：OpenAPI 包含创建房间请求/响应和 `400` 错误契约。
- [ ] AC9：自动测试覆盖成功、非法昵称、昵称过长、房间名过长和邀请码字符规则。
- [ ] AC10：`go test ./services/api/...` 通过。

## Out of Scope

- 加入临时房间。
- 签发 LiveKit token 或 room session token。
- WebSocket 广播或房间实时状态流。
- 10 人容量、30 秒重连、30 分钟空房过期的完整行为实现。
- 固定房间、房间历史、房间密码、房主管理、账号系统。
- 桌面端 UI、LiveKit 客户端、托盘或按键说话改动。

## Open Questions

无阻塞性开放问题；现有 Issue、PRD、design、ADR 与当前代码足以进入设计和实现。
