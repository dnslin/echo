# Issue 16 技术设计：WebSocket 静音、说话与重连状态广播

## 1. 问题重述

当前后端 WebSocket 只覆盖房间快照、成员加入/离开和 heartbeat，仍不能回答房间 UI 的核心问题：谁静音、谁正在说话、谁断线重连中，以及重连中成员是否仍占用房间容量。

## 2. 第一性原理分析

### 2.1 需要先挑战的默认假设

1. **“重连中一定要持久化完整 deadline 才能实现。”**
   - 未验证。
   - 当前系统是单实例 MVP，重连窗口本质上是短生命周期的传输状态，不是长期业务事实。
2. **“说话状态应该由服务端检测音频后决定。”**
   - 明确错误。
   - 产品和 Issue 都已限定 speaking 只是 UI 信号，不做服务端音频检测。
3. **“WebSocket 断开就等于成员立即离房。”**
   - 与产品要求冲突。
   - MVP 需要 30 秒重连窗口，且重连中成员继续占用房间容量。
4. **“既然已有 `member.voice_mode_changed` 契约，就应该顺带一起做。”**
   - 这是类比式扩范围，不是当前任务的必需事实。
   - 当前领域模型只有 `push_to_talk`，Issue #16 验收也不要求 voice mode。

### 2.2 不可再约简的事实

1. 产品房间状态权威属于 API 服务，不属于 LiveKit。
2. WebSocket 意外断开是传输事件，不是显式 leave 命令。
3. 房间容量规则已经把 `online + reconnecting` 视为活跃成员。
4. 当前持久化模型已经有 `members.state`、`muted`、`speaking`，但没有 `reconnect_until` 列。
5. 当前快照投影来自 SQLite 成员行，并按 `joined_at ASC, id ASC` 稳定排序。
6. 本项目是单实例 MVP，Hub 内存状态天然可以承载短时连接信息和 timer。

### 2.3 从事实重新构建方案

由事实 1 和 2 可知：
- “显式 leave” 和 “异常断线重连” 必须走不同状态机。

由事实 3、4、5、6 可知：
- 房间容量和成员身份恢复不能依赖前端本地状态；
- 但 `reconnect_until` 不需要为了一个 30 秒短窗口引入新的 durable schema；
- 最简单的做法是：**持久化真正会影响房间生命周期与授权的成员事实，重连 deadline 留在 Hub 内存中。**

因此本任务采用：
- `muted`、`speaking`：继续走持久化成员状态；
- `reconnect window deadline`：只保存在 Hub 内存；
- `member.state = reconnecting`：**不新增 durable reconnect deadline 字段**，而是由 Hub 在快照/广播层做 reconnecting 投影；
- `member timeout -> disconnected`：超时点再写入持久层，复用现有 leave 生命周期写入能力来释放容量并启动空房保留。

### 2.4 与常见做法的差异

常见直觉是“既然有重连语义，就把所有 reconnect 元数据都写进数据库”。

这里不采用该路径，因为当前 MVP 的约束不是“跨实例精确恢复短时 transport state”，而是“单实例下用最少机制保证用户看到正确房间状态”。把 30 秒 timer 做成 durable schema 会扩大迁移、测试和恢复复杂度，但不会实质提升当前单实例 MVP 的核心用户价值。

## 3. 结论

**本任务的最小正确方案是：用 Hub 内存管理 reconnect window，用 room/store 持久化 mute、speaking 和超时后的 disconnected 结果，通过快照投影层把两者合成为对客户端可见的权威房间状态。**

这比“一切都持久化”更简单，也更符合当前仓库的单实例 MVP 边界。

## 4. 范围内 / 范围外

### 范围内

- `member.mute_changed`
- `member.speaking_changed`
- `member.reconnecting`
- `member.restored`
- `member.disconnected`
- reconnecting 成员继续计入人数上限
- speaking 变化不改变成员排序

### 范围外

- `member.voice_mode_changed`
- `free_talk` 领域建模
- 前端 reducer / UI 实现
- LiveKit 媒体 reconnect 逻辑
- Redis / 跨实例广播
- 服务端音频检测
- 跨设备恢复

## 5. 设计决策

## 5.1 状态所有权

### Durable product facts（持久化）

继续由 SQLite `members` 表保存：
- `state = online | disconnected`
- `muted`
- `speaking`
- `voice_mode`（本任务不扩）

### Runtime transport facts（内存）

只由 `ws.Hub` 保存：
- 当前 room 的活跃 WebSocket 连接
- `seq`
- `memberID -> reconnect deadline`
- reconnect timer
- speaking 节流/去重辅助状态

## 5.2 重连窗口设计

为每个 room 增加内存态：

```text
roomConnections
  seq
  live connections
  reconnecting[member_id] = {
    deadline
    timer
  }
```

语义：
- 连接意外断开时，若不是显式 leave/被替换连接，则进入 reconnecting map。
- reconnecting map 中的成员在快照投影时表现为：
  - `state = reconnecting`
  - `reconnect_until = deadline`
  - `speaking = false`
- 30 秒内同一 `member_id` 重新握手成功，则从 reconnecting map 移除并广播 `member.restored`。
- 30 秒超时仍未恢复，则调用 room service 把成员落为 `disconnected`，并广播 `member.disconnected`。

## 5.3 为什么不新增 `reconnect_until` 列

- deadline 是 30 秒 transport timer，不是长期业务记录；
- 当前快照和连接管理都已经在 Hub 内完成，内存层天然适合承载这类短时状态；
- 只要超时后的 durable 结果（`disconnected` 与空房保留）被正确写回，产品行为就成立；
- API 服务重启会失去当前 reconnect window，这与单实例 MVP 的“实时连接状态在内存中”边界一致。

## 5.4 mute / speaking 变更路径

新增 Hub -> room service 的状态变更接口，而不是让 Hub 直接写 repository。

原因：
- WebSocket 仍是 transport 层；
- 产品规则（谁能改、何时忽略 speaking=true）应由 room service 统一拥有；
- store 只负责持久化，不负责业务语义。

建议新增 room service 能力：
- `UpdateMemberMuteContext(roomID, memberID, muted)`
- `UpdateMemberSpeakingContext(roomID, memberID, speaking)`
- `DisconnectMemberContext(roomID, memberID, at)`

其中：
- `UpdateMemberMuteContext` 写持久层 `muted`；
- `UpdateMemberSpeakingContext` 负责 speaking 去重与基本合法性（例如 muted 时忽略 `speaking=true`）；
- `DisconnectMemberContext` 复用现有 leave 生命周期的 durable 效果：成员 `disconnected`、`speaking=false`、必要时启动空房保留。

## 5.5 显式 leave 与异常断线分流

### 显式 leave

沿用当前 Issue #15 路径：
- HTTP `leave`
- durable `disconnected`
- 广播 `member.left`
- 关闭该连接
- **不进入 reconnecting**

### 异常断线

新路径：
- 连接关闭
- 若不是显式 leave / 被新连接替换，则：
  - 先把 speaking durable 清成 `false`（若原值为 true）；
  - 广播 `member.speaking_changed(false)`（仅在需要时）；
  - 放入 reconnecting map；
  - 广播 `member.reconnecting`。
- 30 秒内恢复则 `member.restored`；
- 超时则 durable `disconnected` + 广播 `member.disconnected`。

## 5.6 speaking 节流策略

目标不是“精准采样语音电平”，而是“防止事件风暴”。

本任务采用简单确定性策略：
- 重复上报相同 speaking 值时直接 no-op；
- 对同一成员的 accepted speaking transition 增加一个小的最小间隔（实现时可用 100ms 级别常量）；
- 被节流的 speaking 变化不报错，只忽略。

原因：
- speaking 是 UI 辅助信号，不是审计/权限信号；
- 稳定和低噪声比“每个毫秒级切换都广播”更重要。

## 6. 跨层数据流

## 6.1 mute change

```text
client command(member.mute_changed)
  -> ws.Hub 验证 envelope
  -> room.Service.UpdateMemberMuteContext
  -> store.Repository.UpdateMemberMute
  -> ws.Hub 广播 member.muted_changed
  -> 后续 room.snapshot 反映最新 muted
```

## 6.2 speaking change

```text
client command(member.speaking_changed)
  -> ws.Hub 验证 envelope + 节流
  -> room.Service.UpdateMemberSpeakingContext
  -> store.Repository.UpdateMemberSpeaking
  -> ws.Hub 广播 member.speaking_changed
  -> reducer/UI 只改 speaking，不改成员顺序
```

## 6.3 unexpected disconnect -> reconnecting -> restored/disconnected

```text
transport close
  -> ws.Hub 判断是否为意外断线
  -> room.Service.UpdateMemberSpeakingContext(... false) [needed only if currently true]
  -> ws.Hub 保存 reconnect deadline(timer)
  -> ws.Hub 广播 member.reconnecting

reconnect before deadline
  -> same ws handshake + same room/member authorization
  -> ws.Hub 识别 reconnecting map
  -> 先发送 room.snapshot
  -> 再广播 member.restored

deadline reached
  -> room.Service.DisconnectMemberContext
  -> store.Repository durable disconnected + retention side effects
  -> ws.Hub 广播 member.disconnected
```

## 7. 文件级改动计划

### `services/api/internal/ws/hub.go`

- 扩展 `Config`：加入 room-state mutation 依赖。
- 为 `roomConnections` 增加 reconnecting runtime state。
- 在 `readLoop` 中支持：
  - `member.mute_changed`
  - `member.speaking_changed`
- 在连接关闭流程里区分：
  - explicit leave / replaced connection
  - unexpected disconnect
- 增加 shared broadcasts：
  - `member.muted_changed`
  - `member.speaking_changed`
  - `member.reconnecting`
  - `member.restored`
  - `member.disconnected`
- 快照构建时 overlay reconnecting 成员的 `state` 与 `reconnect_until`。

### `services/api/internal/room/service.go`

- 新增成员状态更新接口与产品规则。
- 保持 authorization 语义不变：durable `online` / `reconnecting` 才允许握手；若本任务采用 reconnecting 仅内存投影，则授权仍基于 durable active member。
- 超时断开路径通过 room service 统一复用 leave 生命周期副作用，而不是让 Hub 直接碰 repository。

### `services/api/internal/store/sqlite.go`

- 新增成员状态写方法：
  - update muted
  - update speaking
  - timeout disconnect durable transition（可复用/包装现有 leave mutation）
- 不新增 `reconnect_until` 列。
- 保持成员排序和容量统计语义不变。

### `services/api/internal/ws/hub_test.go`

新增或扩展集成测试覆盖：
- mute 广播
- speaking 广播
- speaking 不重排
- unexpected disconnect -> reconnecting
- reconnect within 30s -> restored same member
- timeout -> disconnected
- reconnecting counts toward capacity
- explicit leave still emits left, not reconnecting

### `services/api/internal/store/*_test.go`

新增 repository tests：
- update mute round-trip
- update speaking round-trip
- timeout disconnect keeps existing leave/retention semantics

### `services/api/internal/room/*_test.go`

新增 service tests：
- muted member cannot become speaking=true
- disconnected member cannot update speaking/mute
- timeout disconnect maps repository/domain errors correctly

### `services/api/cmd/api/main.go`

- 将新的 room-state mutation service 依赖注入 Hub。

## 8. 兼容性与风险

## 8.1 向后兼容

- 现有握手、snapshot、join/leave、heartbeat 行为必须保持。
- 现有 HTTP create/join/leave 响应格式不变。
- `member.joined` / `member.left` seq 语义不变。

## 8.2 已知风险

1. **Hub 内存 reconnect window 在服务重启后丢失**
   - 接受。
   - 这是单实例 MVP 的边界；服务重启本就会失去 live transport state。
2. **speaking 已持久化，和“纯 runtime 信号”存在张力**
   - 当前代码已经这样建模。
   - 本任务不再扩大 durable surface，只沿用既有字段并确保断线时能清零。
3. **close path 易把 explicit leave 与 unexpected disconnect 混淆**
   - 必须在 connection 生命周期中引入“禁止启动 reconnect 流程”的显式标志。

## 9. 回滚策略

若实现中发现 reconnect overlay 过于复杂，可回滚到以下最小安全点：
- 先交付 `member.mute_changed` 与 `member.speaking_changed`；
- 保持 unexpected disconnect 只断 transport、不写产品 reconnect state；
- 但这将不能满足 Issue #16 的完整验收，因此只作为开发中的临时回退，不是可接受最终态。

## 10. 验证策略

自动化验证必须覆盖：
- happy path：mute、speaking、reconnect restore
- edge cases：speaking 重复上报、speaking 不重排、reconnecting 仍计容量
- error handling：非法消息、无效状态更新、超时后恢复失败
- state transitions：online -> reconnecting -> online，online -> reconnecting -> disconnected，online -> left

全量命令：

```bash
go test -count=1 ./services/api/...
git diff --check
```
