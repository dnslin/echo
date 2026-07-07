# 验证音频设备选择和系统托盘保持运行

## Goal

验证 GitHub Issue #7（S03）中的高风险路径：在 Wails 3 + WebView2 桌面壳内，前端能通过浏览器媒体 API 枚举麦克风/输出设备、显示当前麦克风输入音量、尽可能切换输出设备；Go/Wails 层能让点击窗口关闭按钮进入系统托盘而不是销毁 WebView，从而不主动中断正在运行的测试音频/媒体进程。

用户价值：在正式设置页、正式房间语音和后续 MVP 功能投入前，先确认音频设备能力与托盘生命周期不会推翻架构。

## Background and Confirmed Facts

- Issue #7 是 #5 的 P0/HITL spike，#6 已关闭，当前任务解除阻塞；验收要求包括枚举麦克风、显示输入音量、输出设备可切换或记录可接受降级、关闭窗口后仍在托盘运行、测试音频连接不应因关闭主窗口直接中断。
- 根 PRD 将麦克风/输出设备选择、输入音量条和关闭窗口进系统托盘列为 MVP 范围：`prd.md:101`、`prd.md:121`、`prd.md:122`、`prd.md:478`、`prd.md:505`、`prd.md:577`。
- 根 PRD 明确排除输出测试音和单人成员音量：`prd.md:142`、`prd.md:474`。
- 根 design 明确 Go shell 负责托盘/窗口生命周期，LiveKit JS/WebView2 负责音频采集/播放，输出设备选择是必做 spike，失败时不能转向 Go 音频管线：`design.md:59`、`design.md:85`、`design.md:127`、`design.md:199`、`design.md:210`。
- ADR 约束：Wails 3 是桌面壳且不回退 Electron（`docs/adr/0005-wails3-desktop-shell.md:1`）；音频采集/播放保留在 WebView2 + LiveKit JS，不新增 Go 音频采集/播放/混音管线（`docs/adr/0007-webview-livekit-audio-devices.md:1`）；本地设置未来保存设备偏好但不保存关闭窗口行为偏好（`docs/adr/0017-local-settings-in-user-config.md:1`）；桌面媒体行为需要 Windows HITL 验收（`docs/adr/0029-layered-testing-with-manual-desktop-media-acceptance.md:1`）。
- 当前桌面端是 Wails/React 骨架，没有设备、托盘、LiveKit 或房间功能：`apps/desktop/README.md:15`；入口只创建一个 WebviewWindow：`apps/desktop/main.go:18`。
- Wails v3 本地依赖已确认支持窗口 `Hide/Show`、系统托盘、菜单点击、`events.Common.WindowClosing` + `RegisterHook` 取消关闭：本地模块 `pkg/application/window.go:29`、`pkg/application/systemtray.go:156`、`internal/commands/appimage_testfiles/main.go:63`。

## Requirements

- R1 麦克风枚举：提供一个 spike 页面，用户可请求麦克风权限并枚举 `audioinput` 设备；授权后展示设备 label，未授权或无设备时展示明确状态。
- R2 输入音量：用户选择/授权麦克风后，页面使用当前本地 media stream 计算输入电平并显示可视化音量条；该路径只测试输入，不做本地回放。
- R3 麦克风切换：用户选择不同麦克风后，应停止旧输入流并用所选 `deviceId` 重新采集；不可用或权限失败时显示错误且不崩溃。
- R4 输出设备验证：页面枚举 `audiooutput` 设备；如果 WebView2 暴露 `setSinkId`，允许对验证用 audio 元素应用所选输出设备并显示成功/失败；如果不支持，则记录“仅跟随系统默认输出设备”的可接受降级，不引入 Go 音频播放管线。
- R5 托盘生命周期：Go/Wails 层创建系统托盘入口；点击主窗口关闭按钮应隐藏窗口并取消默认关闭；托盘菜单至少提供“显示主窗口”和“退出 echo”。
- R6 隐藏不中断：隐藏主窗口不得主动卸载前端页面或停止当前测试媒体流；恢复窗口后 spike 页面能继续显示运行状态，文档记录人工验证结果。
- R7 结果记录：创建 `docs/spikes/device-tray.md`，记录设备枚举、输入电平、输出设备支持/降级、托盘隐藏/恢复、关闭窗口时媒体状态的验证结果和后续约束。
- R8 边界保持：不得实现正式设置页、输出测试音、单人成员音量、房间成员音量、服务端房间、正式 LiveKit 房间流程或 Go 音频采集/播放管线。

## Acceptance Criteria

- AC1 麦克风设备：在 Windows WebView2/Wails dev 或 build 运行中，授权后能看到至少系统暴露的麦克风列表；没有设备或权限失败时有明确文案。
- AC2 输入电平：选择可用麦克风后，音量条随当前输入变化；静音/无输入时数值接近 0；切换麦克风后仍使用当前选择。
- AC3 输出设备：如果 WebView2 暴露 `audiooutput` 和 `setSinkId`，页面可以选择并应用输出设备；如果不暴露，`docs/spikes/device-tray.md` 必须记录不可行原因和“跟随系统默认输出设备”的降级方案。
- AC4 托盘：点击窗口 X 后进程仍在、托盘图标/菜单可用；“显示主窗口”能恢复窗口，“退出 echo”能终止应用。
- AC5 不中断：关闭主窗口进入托盘不会直接调用前端清理逻辑或销毁 WebView；恢复后页面运行计时/媒体状态仍可见，HITL 记录实际结果。
- AC6 自动化测试：前端设备枚举、权限失败、输入电平计算、输出设备 unsupported/supported 分支均有 Vitest 覆盖并通过。
- AC7 构建/静态验证：`npm run test:run`、`npm run build`、`go test ./...`（在对应模块内）通过；`wails3 build` 执行并记录结果，如环境缺少 GUI/工具链则记录阻塞输出。
- AC8 文档：`docs/spikes/device-tray.md` 包含 Result、Validated、Output device result、Tray result、Fallback/Follow-up constraints，不遗漏失败或待 HITL 项。

## Test Scenarios

- T1 Happy path：`enumerateDevices` 返回 1 个麦克风和 1 个输出设备，授权成功，页面展示设备并启动输入电平。
- T2 Empty devices：授权成功但设备列表为空，页面显示无可用麦克风/输出设备状态。
- T3 Permission denied：`getUserMedia` 拒绝时显示“无法使用麦克风，请检查系统权限”，不启动音频分析。
- T4 Microphone switch：选择第二个麦克风时旧 stream tracks 被 stop，新 stream 使用所选 `deviceId`。
- T5 Level meter boundary：输入采样为静音时电平为 0；输入采样为非零时电平升高且被限制在 0-100。
- T6 Output unsupported：`setSinkId` 不存在时，页面显示不支持并提示跟随系统默认输出设备。
- T7 Output supported failure：`setSinkId` 存在但 reject 时，页面显示切换失败且保留选择状态。
- T8 Tray manual：Wails 窗口 X 隐藏窗口；托盘恢复窗口；托盘退出关闭应用。
- T9 Hidden media manual：启动输入电平后隐藏窗口，等待后从托盘恢复，确认页面没有被重新加载且媒体状态未被主动清理。

## Out of Scope

- 正式设置页或设置抽屉。
- 输出设备测试音、本地回放测试或远端成员播放验证。
- 单人成员音量、本地静音某个成员、成员操作菜单。
- 服务端 API、产品房间、正式 LiveKit token/room 流程。
- Go/Wails 音频采集、编码、播放、混音或替代 WebView2 的音频管线。

## Open Questions

无阻塞开放问题。输出设备不可切换时的降级按 ADR 和 Issue 目标处理为：记录不可行原因，MVP 暂时跟随系统默认输出设备，不新增第二套音频管线。
