# Push-to-talk Keyboard press/release Spike

Result: pending Windows HITL

## 当前实现状态

本 spike 为 Issue #9 提供默认按键说话快捷键 `V` 的 press/release 验证路径。

- Windows Go 层安装 low-level keyboard hook，只过滤目标键 `V`。
- Go 层只产生按下/释放事件，不采集、不编码、不播放音频。
- Wails custom event bridge 使用稳定事件名 `keyboard:push-to-talk`。
- 前端 `KeyboardSpike` 通过 `@wailsio/runtime` 的 `Events.On` 订阅 native 事件。
- 前端保留 DOM `keydown` / `keyup` fallback，只用于 echo/WebView 聚焦时的普通桌面对照。
- UI 显示当前目标键、按下次数、释放次数、完整循环、最近事件，以及按下未释放警告。

## 自动验证结果

以下自动验证已在本次检查中运行并通过：

- `npm --prefix apps/desktop/frontend run test:run`: pass。6 个测试文件、31 个测试通过，覆盖 10 次 DOM fallback press/release、缺失 release 警告、native Wails event payload、reducer 10 循环、重复 keydown 忽略、非目标键忽略和 malformed payload 忽略。
- `npm --prefix apps/desktop/frontend run build`: pass。TypeScript 与 Vite production build 通过。
- `go -C apps/desktop test ./...`: pass。覆盖 keyboard transition tracker，并验证 desktop Go packages 可编译。
- `cd apps/desktop && wails3 build`: pass。Wails v3.0.0-alpha2.115 构建完成并生成 `apps/desktop/bin/echo.exe`。

## Windows HITL 验证步骤

1. 在 `apps/desktop` 运行 `wails3 dev`。
2. 确认窗口显示“按键说话 press/release 验证”。
3. 普通桌面对照：聚焦 echo 窗口，连续按下并释放 `V` 10 次。
4. 确认 UI 显示 `按下次数：10`、`释放次数：10`、`完整循环：10`，并且没有“检测到按下未释放”提示。
5. 游戏前台验证：切换到无边框或窗口化游戏前台，连续按下并释放 `V` 10 次。
6. 确认最近事件中出现 native 事件，并且完整循环仍为 10。
7. 如有可用全屏/独占模式游戏，重复 10 次 press/release 并记录结果。
8. 如有可用管理员权限游戏，分别记录普通权限 echo 与同权限 echo 的表现；只记录权限边界，不做自动提权。
9. 如有安全且已安装的反作弊游戏可测，记录是否受限；不得尝试规避反作弊。
10. 记录 Windows 版本、游戏名称/窗口模式、echo 权限级别、结果和失败原因。

## Windows HITL 场景矩阵

| 场景 | 期望 | 当前结果 | 记录 |
| --- | --- | --- | --- |
| 普通桌面 / echo 聚焦 | 10 次 `V` press/release 得到 10 个完整循环，无 missing release | not tested | 当前只完成自动验证；等待人工 Windows HITL。 |
| 无边框或窗口化游戏前台 | 游戏为前台时 native 事件仍得到 10 个完整循环 | not tested | 等待人工 Windows HITL。 |
| 全屏 / 独占游戏前台 | 尽量验证 10 个完整循环；失败或不可测时记录原因 | not tested | 等待有可用游戏后复测。 |
| 管理员权限游戏 | 如果普通权限 echo 收不到事件，记录 Windows 权限边界；可建议同权限运行，不做绕过 | not tested | 等待人工 Windows HITL。 |
| 反作弊限制游戏 | 如果事件被限制，记录为已知限制；不得静默写 pass | not tested | 等待安全可测目标后复测。 |

## 兼容性结论

当前只能给出自动验证结论：代码路径、状态推导、Wails event bridge 使用方式和 Windows 构建均已通过自动验证。Issue #9 的核心结论仍依赖人工 Windows HITL；只有普通桌面与无边框/窗口化游戏前台各 10 次 press/release 通过后，才可把本 spike 标记为 `pass`。

已知边界：

- 普通权限进程可能无法观察更高完整性级别进程中的输入。
- 反作弊软件可能限制或阻止全局 hook。
- MVP 不承诺所有反作弊游戏、管理员权限游戏、远程桌面或虚拟机 100% 可用。
- 本 spike 不实现正式语音发送、LiveKit mute/unmute、房间状态、快捷键编辑器、Overlay、自动提权或反作弊规避。

## 失败 stop rule

如果人工 HITL 出现以下情况，应将结果记录为 `fail` 或 `partial`，并准确记录原因：

- 普通桌面聚焦 echo 时无法稳定识别 `V` press/release。
- 无边框/窗口化游戏前台无法稳定识别连续 10 次 press/release，且已排除权限不一致、目标游戏输入限制和测试步骤错误。
- release 丢失导致 UI 长时间停留在“按下”状态。
- 为通过测试需要增加正式语音发送、Go 音频路径、自动提权或反作弊规避。
