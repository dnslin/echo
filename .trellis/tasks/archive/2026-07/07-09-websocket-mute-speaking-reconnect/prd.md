# Issue 16 WebSocket 静音、正在说话和重连状态广播

## Goal

实现 Issue #16 的后端房间状态广播扩展：在现有房间 WebSocket 基础上补齐静音状态、正在说话状态、WebSocket 断连后的 30 秒重连窗口，以及重连失败后的移除广播，让房间 UI 能可靠回答“谁正在说话、谁静音、谁重连中、房间人数是否已满”。

用户价值：进入临时房间后，成员列表不仅能显示加入/离开，还能持续反映真实的产品房间状态变化，避免用户误判“朋友还能不能听见我”或“某人是否已经掉线”。

## Source issue

- GitHub Issue #16: `[S11] WebSocket 静音、正在说话和重连状态广播`
- Parent epic: Issue #3
- Blocked by issue #15; 当前仓库已完成 Issue #15 的房间快照、加入/离开广播与基础 heartbeat。

## Confirmed facts

- 产品需求明确要求房间成员列表展示谁正在说话、谁静音、谁重连中，并要求重连中成员保留位置且计入房间人数上限（`prd.md:100-105`, `prd.md:348-356`, `prd.md:452-476`, `prd.md:531-576`）。
- WebSocket 契约已经定义了 `member.muted_changed`、`member.speaking_changed`、`member.reconnecting`、`member.restored`、`member.disconnected` 及相应客户端命令/重连语义；Issue #16 是把这部分契约落成服务端实现，而不是重新发明协议（`docs/api/websocket.md:309-417`, `docs/api/websocket.md:509-578`, `docs/api/websocket.md:608-627`）。
- 现有 WebSocket hub 已支持：握手鉴权、`room.snapshot`、`member.joined`、`member.left`、`heartbeat.ping`、`heartbeat.pong`、`room.resync_requested`、`room.error`，但对 `member.mute_changed` / `member.speaking_changed` 仍返回 unknown message，不会修改产品状态（`services/api/internal/ws/hub.go:260-303`, `services/api/internal/ws/hub.go:564-615`, `services/api/internal/ws/hub_test.go:494-526`）。
- 现有 HTTP create/join/leave 与 WebSocket 事件通知只会触发 joined/left；还没有静音、说话、重连相关的 notifier 接口或状态广播入口（`services/api/internal/http/handlers.go:36-37`, `services/api/internal/http/handlers.go:192-229`）。
- 领域模型已经包含 `MemberStateReconnecting`、`Muted`、`Speaking`，说明“重连中/静音/正在说话”是既定产品状态；但 `domain.VoiceMode` 当前只有 `push_to_talk`，`Member` 也还没有 `ReconnectUntil` 字段（`services/api/internal/domain/types.go:23-35`, `services/api/internal/domain/types.go:52-65`）。
- 当前活跃成员判定已经把 `online + reconnecting` 视为活跃集合，容量统计与成员授权都依赖这一规则，因此 Issue #16 不能把重连中成员提前释放出房间容量（`services/api/internal/room/service.go:313-316`, `services/api/internal/room/service.go:474-485`, `services/api/internal/store/join_test.go:43-69`, `services/api/internal/room/service_join_test.go:292-296`）。
- 持久化层当前在显式 leave 时会直接把成员标为 `disconnected` 并清空 `speaking`，如果房间因此变空则启动 30 分钟保留；这说明“异常断开进入 reconnecting、30 秒后再转 disconnected”这段生命周期尚未落地（`services/api/internal/store/sqlite.go:252-352`）。
- 现有成员投影已经包含 `muted`、`speaking`、`voice_mode`，但 `ReconnectUntil` 始终投影为 `nil`；Issue #16 需要让 reconnecting 快照和事件与契约一致（`services/api/internal/ws/hub.go:126-138`, `services/api/internal/ws/hub.go:762-780`）。
- Issue #16 的 GitHub 验收明确要求：静音广播、说话状态广播但不引发列表重排、WebSocket 断线进入重连中、30 秒内恢复原成员身份、超时后移除并广播、重连中成员计入人数上限；同时明确 speaking 只是 UI 信号、不做服务端音频检测、不做跨设备登录恢复。

## Requirements

### R1. Self mute command updates product state and broadcasts authoritative room state

- 已建立连接的成员可以通过 WebSocket 发送自己的 `member.mute_changed`。
- 服务端必须只依据已验证的 room session token 确认“是谁在改自己的 mute”，不得信任 payload 中的 `member_id`。
- 服务端接受后必须持久化该成员的 `muted` 状态，并向房间内其他已连接成员广播 `member.muted_changed`。
- 广播后的后续 `room.snapshot` 也必须反映最新 `muted` 状态。
- 静音是产品房间权威状态；客户端可以乐观更新自己的 UI，但最终以服务端广播为准。

### R2. Self speaking signal updates product state and broadcasts without reordering members

- 已建立连接的成员可以通过 WebSocket 发送自己的 `member.speaking_changed`。
- speaking 只是产品 UI 信号，不是鉴权、计费或音频检测依据。
- 服务端不得做服务端音频检测；只消费客户端上报的 speaking 布尔变化。
- 服务端必须节流/去重 speaking 变化，避免事件风暴或重复广播无效状态。
- 服务端接受后必须更新产品状态并广播 `member.speaking_changed`。
- 成员列表的稳定排序不能因为 speaking 变化而改变；广播只改变状态，不改变顺序。

### R3. Unexpected WebSocket disconnect enters reconnecting state instead of immediate removal

- 当房间成员的 WebSocket 连接意外断开且不是显式 leave 流程时，服务端必须把该成员转为 `reconnecting`。
- 成员进入 `reconnecting` 时必须保留原 `member_id`、原列表位置和房间容量占位。
- 进入 `reconnecting` 时必须清空其 `speaking` 状态，避免其他成员继续看到它在说话。
- 服务端必须广播 `member.reconnecting`；如果该成员此前处于 `speaking=true`，还必须对外体现为 speaking 被清除。
- reconnecting 成员必须继续被视为 active member，用于容量判断和快照成员列表。

### R4. Reconnect within 30 seconds restores the original member identity

- 在 30 秒重连窗口内，原成员重新建立房间 WebSocket 连接时，服务端必须恢复原 `member_id` 与原房间身份，而不是创建新成员。
- 恢复后服务端必须广播 `member.restored`，并让后续 `room.snapshot` 显示该成员回到 `online`。
- 恢复流程不得依赖 LiveKit participant presence 来推断产品房间身份。
- 若恢复成功，房间容量占位和列表顺序必须连续，不出现“先移除再新增”的跳变。

### R5. Reconnect timeout removes the member and frees capacity

- 若 30 秒内没有恢复连接，服务端必须把该成员从 `reconnecting` 转为 `disconnected`。
- 服务端必须广播 `member.disconnected`，并从后续快照与活跃成员列表中移除该成员。
- 成员移除后必须释放房间容量；若房间因此变空，则沿用现有空房保留/过期逻辑。

### R6. Explicit leave remains an immediate leave, not a reconnect flow

- 通过 HTTP leave 或等价显式离房路径离开的成员，仍应保持 Issue #15 语义：广播 `member.left` 并结束该成员连接。
- 显式 leave 不得先进入 `reconnecting`，否则会错误占用房间名额并干扰 30 分钟空房保留逻辑。

### R7. Scope and boundary constraints

- 不做服务端音频检测。
- 不做跨设备登录恢复。
- 不让 LiveKit 媒体连接状态直接覆盖产品房间状态权威性。
- 不实现 chat、房主管理、Redis/pubsub、跨实例广播。
- 不在本 issue 中要求桌面前端 UI 改动；本 issue 聚焦服务端产品状态与 WebSocket 广播。

## Acceptance Criteria

- [ ] AC1: 成员发送 `member.mute_changed` 后，服务端更新其产品 `muted` 状态，并向同房间其他成员广播 `member.muted_changed`。
- [ ] AC2: 重新请求 `room.snapshot` 时，成员的 `muted` 状态与最近一次服务端接受的值一致。
- [ ] AC3: 成员发送 `member.speaking_changed` 后，服务端按节流/去重规则广播 speaking 变化。
- [ ] AC4: speaking 状态变化不会造成成员列表重排；快照和增量事件都保持稳定成员顺序。
- [ ] AC5: WebSocket 意外断开后，成员进入 `reconnecting`，其他成员能看到该状态，且其 `speaking` 状态被清空。
- [ ] AC6: reconnecting 成员在 30 秒窗口内重新连回时，恢复原 `member_id` 和原列表位置，并广播 `member.restored`。
- [ ] AC7: reconnecting 成员超过 30 秒未恢复时，被广播为 `member.disconnected` 并从活跃成员列表移除。
- [ ] AC8: reconnecting 成员在窗口期间继续计入房间人数上限；房间未满前提下的计数逻辑仍按 `online + reconnecting` 计算。
- [ ] AC9: 超时移除 reconnecting 成员后，房间容量被释放；若房间因此变空，现有空房保留/过期逻辑继续正确工作。
- [ ] AC10: 显式 leave 仍广播 `member.left`，不会误进入 reconnecting 流程。
- [ ] AC11: 建立连接后的未知/非法消息仍不会错误修改房间状态；仅支持本 issue 范围内的命令类型。
- [ ] AC12: 自动化测试至少覆盖 happy path、边界与错误场景：mute 广播、speaking 广播、speaking 不重排、断连进 reconnecting、30 秒内恢复、30 秒超时移除、reconnecting 计入人数、leave 不走 reconnecting、非法消息不越权修改状态。
- [ ] AC13: 现有 Issue #15 的 WebSocket 握手、快照、join/leave 广播、heartbeat 与现有 HTTP/room/store 测试持续通过。

## Out of scope

- `member.voice_mode_changed` 的完整产品落地，以及 `free_talk` 领域枚举和相关持久化/广播。
- 桌面端 reducer、房间 UI、LiveKit 本地 track 控制、托盘与设备行为。
- 基于媒体层的断线探测、弱网策略、TURN、跨实例状态同步。
- 账号体系、跨设备恢复、房主管理、聊天、固定房间或治理功能。

## Open questions

None blocking after repository inspection. Scope decision is fixed: Issue #16 implements only `mute`, `speaking`, and `reconnect` room-state behavior; `member.voice_mode_changed` / `free_talk` stays out of scope for this task.
