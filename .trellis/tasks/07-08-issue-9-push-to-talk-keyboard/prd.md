# [S04] 验证游戏前台按键说话 press/release

## Goal

验证 echo Windows 桌面端在普通桌面和游戏前台场景中能可靠识别按键说话快捷键的按下与松开，为后续正式语音发送逻辑提供可执行的原生按键事件路径和 HITL 兼容性记录。

## Background / Confirmed Facts

- GitHub Issue #9：`[S04] 验证游戏前台按键说话 press/release`，类型为 P0 spike，目标是验证 Windows 桌面和游戏前台场景中的 press/release 可靠性。
- Issue #9 blocked by #6；GitHub Issue #6 当前为 `CLOSED`，工作区、工具链和契约骨架前置条件已解除。
- 根 `prd.md` 要求：默认按键说话快捷键为 `V`；按下快捷键满足发送条件，松开停止发送；echo 在后台、游戏在前台时仍应可识别；MVP 不承诺所有反作弊游戏、管理员权限游戏、远程桌面或虚拟机 100% 可用。
- 根 `design.md` 要求：Wails Go shell 负责按键 hook 和向前端桥接原生事件；如果 Wails 快捷键 API 不能可靠表达 keydown/keyup，则使用 Windows low-level keyboard hook；Go 层不采集、不编码、不播放语音。
- 根 `implement.md` Task 4 是本任务直接来源：创建 `apps/desktop/internal/keyboard/*`、`frontend/src/spike/KeyboardSpike.tsx`、路由到 spike、记录 `docs/spikes/push-to-talk-keyboard.md`。
- 已完成的前置 spike：`docs/spikes/wails-livekit-audio.md` 为 pass；`docs/spikes/device-tray.md` 为 pass。当前 `apps/desktop/main.go` 已有 Wails 3 窗口、托盘、close-to-tray 行为。
- 当前前端仍路由到 LiveKit audio spike；本任务可临时改为 keyboard spike 页面，但不得实现正式房间、正式语音发送、快捷键复杂编辑器或绕过反作弊限制。

## First-Principles Requirement Analysis

### Challenge assumptions

- 未验证假设：普通浏览器 `keydown` / `keyup` 足以覆盖游戏前台。它只能在 WebView 获得焦点时可靠工作，不能作为后台游戏场景的根机制。
- 未验证假设：Wails global shortcut 能提供松开事件。Issue 的验收需要 press 与 release 成对出现，只能触发快捷键激活的 API 不满足。
- 潜在错误假设：为验证按键说话必须接入正式语音发送。Issue 明确边界是不实现正式语音发送；本任务只需要验证事件路径和记录结果。
- 潜在错误假设：必须支持绕过反作弊或管理员权限隔离。PRD 明确这些是兼容性边界，不允许承诺或规避安全限制。

### Bedrock truths

- 按键说话的最小可验证事实是一个按键每次物理按下产生 `pressed=true`，释放产生 `pressed=false`，并且 UI 能观察事件序列。
- 游戏前台时 WebView 不拥有键盘焦点，因此只依赖浏览器 DOM 键盘事件无法证明后台能力。
- Windows 用户权限边界存在：普通权限进程可能无法观察更高完整性级别进程中的输入；反作弊软件可能限制全局 hook。
- Spike 的输出必须是可复测的代码路径、自动测试结果和不夸大能力的 HITL 文档，而不是口头结论。

### Rebuild from truths

- 后台游戏场景需要 Go/Wails 原生层接收系统级按键事件，并通过 Wails event bridge 把 `key`、`pressed`、时间戳/来源传到 React spike UI。
- 前端 spike UI 必须展示最近事件、down/up 计数、是否存在缺失 release，并保留 DOM fallback 仅用于“应用聚焦时”的对照验证。
- 自动测试覆盖事件归一化、重复 keydown 去重、down/up 计数、缺失 release 提示和 smoke render；真实游戏前台能力只能由 Windows HITL 验证。
- HITL 文档必须把普通桌面、无边框窗口游戏、全屏/独占、管理员权限、反作弊限制分开记录，未知或失败必须写成限制，不能静默省略。

### Contrast with convention

常规捷径可能只做 React `keydown` / `keyup` 页面并在应用聚焦时验证。那只能证明 WebView 焦点内输入有效，不能证明 Issue #9 的核心后台游戏前台风险。根本差异是：本任务的验证对象是“系统前台不属于 echo 时仍能观察 press/release”，因此必须有原生路径和 HITL 记录。

## Requirements

- R1. 提供 Windows 原生按键事件路径，至少覆盖默认快捷键 `V` 的按下与松开，并把事件桥接到前端 spike UI。
- R2. 非 Windows 构建必须保留可编译 stub，不引入跨平台支持承诺。
- R3. 前端提供 `KeyboardSpike` 页面，显示：当前目标键、最近事件、down/up 次数、连续完整循环次数、是否检测到按下未释放、测试说明。
- R4. 应用临时路由到 keyboard spike，用于本 Issue 验证；不得实现正式语音发送、房间、复杂快捷键编辑器或鼠标/手柄扩展。
- R5. 自动测试覆盖普通 DOM fallback 事件处理和 spike UI 状态逻辑，确保 10 次按下/松开可以被计数为 10 个完整循环。
- R6. `docs/spikes/push-to-talk-keyboard.md` 记录自动验证和 Windows HITL 验收清单；在人工验证前不得把未验证场景写为 pass。
- R7. 文档必须明确管理员权限和反作弊限制：如果不可用，记录为已知限制；不得承诺或实现绕过反作弊。

## Acceptance Criteria

- [ ] AC1 → R1/R2：`go test ./...` 在 `apps/desktop` 通过；Windows 文件实现原生 hook 或等价 press/release 事件源，非 Windows stub 可编译。
- [ ] AC2 → R3/R5：`npm run test:run` 在 `apps/desktop/frontend` 通过，覆盖 10 次 `V` down/up 后完整循环数为 10，且缺失 release 会显示明确提示。
- [ ] AC3 → R3/R4：`npm run build` 在 `apps/desktop/frontend` 通过；App smoke test 断言 keyboard spike 可见，不再断言 LiveKit spike 为当前路由。
- [ ] AC4 → R1/R4：`wails3 build` 在 `apps/desktop` 通过并产出 Windows 桌面构建。
- [ ] AC5 → R6：`docs/spikes/push-to-talk-keyboard.md` 存在，包含自动验证结果、Windows HITL 步骤和分场景结果表。
- [ ] AC6 → R6：普通桌面场景连续 10 次按下/松开可识别，结果记录到 spike 文档。
- [ ] AC7 → R6：无边框窗口游戏前台可识别，结果记录到 spike 文档。
- [ ] AC8 → R6/R7：全屏/独占前台游戏尽量验证并记录结果；如无法验证，记录原因和后续复测条件。
- [ ] AC9 → R7：管理员权限游戏场景给出明确兼容性结论；如果普通权限 echo 不可用，记录权限边界和建议同权限启动，不做绕过。
- [ ] AC10 → R7：带反作弊限制的游戏若不可用，记录为已知限制；不得把失败静默写成 pass。

## Out of Scope

- 正式语音发送、LiveKit mute/unmute、房间状态、业务 WebSocket 或成员 speaking 同步。
- 全局快捷键复杂编辑器、组合键完整支持、鼠标侧键、鼠标宏键、手柄、游戏虚拟按键。
- 绕过反作弊、绕过 Windows 权限边界、为管理员权限游戏做提权自动化。
- Overlay、游戏内悬浮窗、正式设置页、正式产品快捷键冲突检测。

## Open Questions

无阻塞规划的问题。HITL 具体游戏名称可在验证时按实际可用游戏记录；不影响实现方案。
