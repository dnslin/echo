# Push-to-talk Keyboard press/release Spike

Result: pending Windows HITL

## 当前实现状态

本 spike 为 Issue #9 提供默认按键说话快捷键 `V` 的 press/release 验证路径。

- Windows Go 层安装 low-level keyboard hook，只过滤目标键 `V`。
- Go 层只产生按下/释放事件，不采集、不编码、不播放音频。
- Wails custom event bridge 使用稳定事件名 `keyboard:push-to-talk`，native payload 带单调递增 `sequence` 供前端重排。
- 前端 `KeyboardSpike` 通过 `@wailsio/runtime` 的 `Events.On` 分别订阅 native press/release 与 `keyboard:hook-status`。
- 前端 mount 后发送 `keyboard:hook-status-request`，UI 至少区分 native hook `enabled`、`disabled` 与非 Windows `unsupported`。
- 前端保留 DOM `keydown` / `keyup` fallback，只用于 echo/WebView 聚焦时的普通桌面对照。
- UI 分开显示 Windows native hook 与 WebView DOM fallback 两张卡片，各自维护按下次数、释放次数、完整循环、最近事件和按下未释放警告；native sequence 出现缺口时会显示已缓冲的乱序事件。

## 自动验证结果

以下自动验证已在本次检查中运行并通过：

- `cd apps/desktop/frontend && npm run test:run`: pass。6 个测试文件、38 个测试通过，覆盖 native 乱序重排、pending sequence 缺口提示、native/DOM 独立计数、reset 后继续接收单调 native sequence、reset 清除 pending 后继续测试、10 次完整循环、重复 keydown 忽略、非目标键忽略、malformed payload 忽略、hook disabled UI 与重置统计。
- `cd apps/desktop/frontend && npm run build`: pass。TypeScript 与 Vite production build 通过。
- `cd apps/desktop && go test ./...`: pass。覆盖 native event sequence、`requestThreadQuit` retry seam、hook status error mapping，并验证 desktop Go packages 可编译。
- `cd apps/desktop && wails3 build`: pass。Wails v3.0.0-alpha2.115 构建完成并生成 `apps/desktop/bin/echo.exe`。

## Windows HITL 验证步骤

1. 在 `apps/desktop` 运行 `wails3 dev`。
2. 确认窗口显示“按键说话 press/release 验证”，并检查 Windows native hook 卡片的状态；若为 `disabled` 或 `unsupported`，记录错误原因，不得把 DOM fallback 结果当作 native 通过。
3. 每一轮测试前点击“重置统计”；如果无法重置，则先记录 native 与 DOM 两张卡片的当前基线，后续只比较本轮增量。
4. 普通桌面对照：聚焦 echo 窗口，连续按下并释放 `V` 10 次。
5. 确认 Windows native hook 与 WebView DOM fallback 两张卡片各自的 `完整循环` 本轮增量；普通桌面对照不能替代游戏前台结论。
6. 游戏前台验证：再次点击“重置统计”，切换到无边框或窗口化游戏前台，连续按下并释放 `V` 10 次。
7. 只读取 Windows native hook 卡片：确认 `按下次数：10`、`释放次数：10`、`完整循环：10`，并且 native 卡片没有“按下未释放”或“等待 native seq”提示；不要使用 DOM fallback 或累计总数判定。
8. 如有可用全屏/独占模式游戏，先重置统计，再重复 10 次 press/release，并只记录 native 卡片结果。
9. 如有可用管理员权限游戏，先重置统计，分别记录普通权限 echo 与同权限 echo 的 native 卡片表现；只记录权限边界，不做自动提权。
10. 如有安全且已安装的反作弊游戏可测，先重置统计，记录 native 卡片是否受限；不得尝试规避反作弊。
11. 记录 Windows 版本、游戏名称/窗口模式、echo 权限级别、hook 状态、native/DOM 本轮计数、结果和失败原因。

## Windows HITL 场景矩阵

| 场景 | 期望 | 当前结果 | 记录 |
| --- | --- | --- | --- |
| 普通桌面 / echo 聚焦 | 重置后 10 次 `V` press/release；native 与 DOM 两张卡片分别记录本轮完整循环，无 missing release | not tested | 当前只完成自动验证；等待人工 Windows HITL。 |
| 无边框或窗口化游戏前台 | 重置后游戏为前台时，Windows native hook 卡片得到 10 个完整循环 | not tested | 等待人工 Windows HITL；DOM fallback 不参与判定。 |
| 全屏 / 独占游戏前台 | 重置后尽量验证 native 卡片 10 个完整循环；失败或不可测时记录原因 | not tested | 等待有可用游戏后复测。 |
| 管理员权限游戏 | 重置后读取 native 卡片；如果普通权限 echo 收不到事件，记录 Windows 权限边界；可建议同权限运行，不做绕过 | not tested | 等待人工 Windows HITL。 |
| 反作弊限制游戏 | 重置后读取 native 卡片；如果事件被限制，记录为已知限制；不得静默写 pass | not tested | 等待安全可测目标后复测。 |

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
