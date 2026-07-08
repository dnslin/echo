# Design: push-to-talk keyboard review fixes

## First-principles reasoning

### Challenge assumptions

- 未验证假设：Wails `Emit` 调用顺序等于 WebView 收到顺序。源码显示该假设错误。
- 未验证假设：DOM fallback 与 native hook 可以共享一个“是否按下”状态。物理事实是 echo 聚焦时两条传感路径可能同时观测同一次按键。
- 未验证假设：日志足以表达 hook 安装失败。HITL 测试者观察的是 UI，不一定读取 Go stdout。
- 未验证假设：一次 unhook 就足以停止 hook 线程。`GetMessageW` 是阻塞等待，线程退出需要消息循环被唤醒或返回。

### Bedrock truths

- Windows hook 在同一个 callback/message loop 中按 OS 观察顺序产生 transition。
- Go channel 在单发送者场景保持发送顺序。
- Wails v3 `EventProcessor.Emit` 为窗口派发启动 goroutine；不同事件的 goroutine 调度顺序不是 payload 顺序保证。
- Browser event listener 只能处理实际到达的数据；若 payload 无 sequence，前端无法重建 native 原始顺序。
- DOM keydown/keyup 和 native hook 是两个来源，不是同一可信观测点。

### Rebuild from ground up

1. Native 事件在离开 Go hook 前必须携带单调递增 `sequence`。
2. 前端对 native 事件按 `sequence` 建立小型 pending buffer，只按 `nextNativeSequence` 顺序应用。
3. DOM fallback 不使用 native sequence；DOM 与 native 各自维护 `isPressed/down/up/completed/missingRelease/events`。
4. Hook status 是独立事件契约，不混入按键 transition；UI 初始化后主动请求当前状态，避免启动事件丢失。
5. Stop 先确保 hook 线程 message queue 存在，再用 `WM_QUIT` 唤醒；失败时 unhook 并重试 post，最后超时记录边界。

### Contrast with convention

常规做法可能直接在 Go 侧“串行调用 Emit”或在前端继续相信到达顺序；这忽略了 Wails 内部 goroutine 派发这一原始约束。最小可靠差异是把顺序变成 payload 数据，而不是把顺序寄托给调度器。

## Event contracts

### Keyboard transition

Go `keyboard.Event` 增加：

- `Sequence uint64 json:"sequence,omitempty"`：native source 必填，DOM source 不使用。
- `source` 继续使用 `native` / `dom`。

Frontend `KeyboardInputEvent` 接受可选 `sequence`。`normalizeKeyboardEventData` 对 native payload 要求 `sequence` 为正整数；DOM payload 不从 Wails 接收。

### Hook status

新增事件常量：

- `keyboard:hook-status`：Go → frontend，payload `{ status: 'enabled' | 'disabled' | 'unsupported', message?: string }`。
- `keyboard:hook-status-request`：frontend → Go，无 payload；Go 收到后重发当前状态。

Windows `Start` 成功为 `enabled`；失败为 `disabled` + 错误；non-Windows 为 `unsupported`。

## Frontend state shape

`KeyboardSpikeState` 拆为：

- `sources.native` 和 `sources.dom`：各自有 `isPressed/downCount/upCount/completedCycles/missingRelease/events`。
- `nativeOrdering`：`nextSequence` 与 pending map/list，用于乱序重放。
- `hookStatus`：当前 hook 状态与可选 message。

UI 显示两张路径卡片：

- Windows native hook：用于游戏前台验证，显示 hook 状态、native counters、native log。
- WebView DOM fallback：仅用于 echo 聚焦对照，显示 DOM counters/log。

提供“重置统计”按钮，重置 counters/log/pending，但保留 hook status。

## Stop behavior

Windows hook run loop 在线程启动后调用一次非阻塞 `PeekMessageW` 或等价 API 以创建 message queue，再设置 `threadID` 并安装 hook。`Stop` 获取 threadID 后：

1. `PostThreadMessageW(threadID, WM_QUIT, 0, 0)`。
2. 如果 post 失败，先 `unhook()` 再短暂重试 post。
3. 等待 `done`，超时后清空状态但不假称线程已退出。

完全模拟 stale Windows thread id 需要 Windows API 注入；本任务自动测试覆盖可拆出的 sequencing/state 逻辑，线程退出保留 `go test` + HITL/手工观察边界。

## Compatibility

- 保持现有事件名 `keyboard:push-to-talk`。
- 新增字段使用 `omitempty`，未来真实房间按键逻辑可复用 sequence/source，但本任务只修 spike。
- 不改变非 Windows no-op 行为；非 Windows UI 应显示 unsupported。

## Risks

- 如果 Wails 窗口事件丢失而不是乱序，sequence buffer 只能显示 pending/gap，不能恢复丢失事件。UI 应通过 missing/pending 状态让 HITL 记录失败。
- `Stop` 的 Windows API 行为难以在非 Windows CI 完全复现；避免过度抽象，只把可测试纯逻辑拆出。
