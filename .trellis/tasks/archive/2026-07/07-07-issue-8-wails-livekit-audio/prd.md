# 验证 Wails 3 + WebView2 + LiveKit 音频路径

## Goal

验证 echo MVP 最高风险媒体假设：Wails 3 桌面壳内的 WebView2 是否可以运行 LiveKit JS，加入同一个测试 LiveKit 房间，发布本地麦克风音频，订阅并播放远端音频，从而支撑后续正式临时房间语音实现。

## Background

- GitHub Issue #8：`[S02] 验证 Wails 3 + WebView2 + LiveKit 音频路径`。
- Issue #8 属于 Epic #5，并被 Issue #6 阻塞；Issue #6 已关闭，工作区与 Wails 桌面骨架已存在。
- 仓库当前已有 `apps/desktop` Wails 3 桌面端、React/Vite/Vitest 前端、系统托盘 close-to-tray 行为，以及 Issue #7 的设备/托盘验证记录 `docs/spikes/device-tray.md`。
- 当前 `apps/desktop/frontend/src/App.tsx` 临时渲染 `DeviceTraySpike`，Issue #8 可以替换为音频路径 spike；该 UI 仍是 spike UI，不是正式房间 UI。
- 依据 `design.md` 与 ADR：语音媒体必须保留在 WebView2 + LiveKit JS 中；Go/Wails 层不采集、不播放、不混音、不编码音频。

## Requirements

### R1. LiveKit audio spike dependency

- 在 `apps/desktop/frontend` 安装并使用 LiveKit JS 客户端依赖。
- 依赖只用于 spike 页面验证 LiveKit 媒体路径，不引入正式产品房间、邀请码、业务 WebSocket 或成员列表。

### R2. Spike UI

- 添加一个可在 Wails WebView2 中运行的 LiveKit 音频 spike 页面。
- 页面必须允许输入 LiveKit URL 和短期 join token。
- 页面必须显示连接状态：未连接、连接中、已连接、失败。
- 页面必须显示可操作错误信息，但不得在日志或文档中保存 token 明文。
- 页面必须提供连接并发布麦克风的操作。
- 页面必须提供断开连接或清理资源的操作，避免测试后继续占用麦克风。

### R3. Media behavior

- 连接成功后，桌面客户端必须向同一 LiveKit 房间发布本地麦克风音频 track。
- 当远端音频 track 订阅成功时，页面必须 attach 并播放远端音频元素。
- 远端音频元素应使用浏览器/WebView2 播放路径，不得经过 Go 音频播放管线。
- 关闭窗口进入托盘后的媒体持续性已由 Issue #7 验证为基础能力；本任务只在音频路径记录中说明该依赖，不重复实现托盘。

### R4. Test environment and HITL evidence

- 验证必须在 Windows 10/11 x64 的 Wails 3 WebView2 桌面窗口中执行。
- HITL 环境采用公网可访问的 LiveKit WSS 服务。
- 验证必须至少使用两个客户端加入同一个测试 LiveKit 房间。
- 第二客户端优先使用 LiveKit 官方/自建测试页或另一台机器上的 LiveKit 客户端，以减少同机双 Wails 实例带来的回声、多实例和设备占用干扰；记录中必须说明实际使用的客户端类型。
- HITL 记录必须包含：Windows 版本、Wails 版本、WebView2 运行环境、公网 LiveKit 服务地址类别（不含 secret）、客户端数量、token 生成方式摘要、通过/失败结果、失败现象、限制和后续约束。

### R5. Failure handling and stop rule

- 若 LiveKit JS 无法在 Wails WebView2 中连接、请求麦克风、发布本地音频、订阅远端音频或播放远端音频，必须将 `docs/spikes/wails-livekit-audio.md` 标记为 `Result: fail`。
- 失败记录必须明确阻断原因和复现条件。
- 若失败阻断 WebView2 + LiveKit 语音路径，后续正式媒体实现必须暂停并回到设计，不得用 Go 音频管线或 Electron 作为本任务内的临时绕过。

## Out of Scope

- 不实现产品临时房间。
- 不实现邀请码、成员列表、重连、静音状态同步、业务 WebSocket 或 API token 签发服务。
- 不实现正式房间 UI、正式设置页或正式 LiveKit wrapper。
- 不实现设备选择；Issue #7 已覆盖设备/托盘验证。
- 不实现 TURN、Redis、PostgreSQL、Electron、服务端混音或 Go 音频采集/播放。
- 不提交真实 LiveKit token、API secret、房间 session secret 或可长期复用的凭据。

## Acceptance Criteria

- [ ] A1. `apps/desktop/frontend/package.json` 包含 LiveKit JS 客户端依赖，且前端依赖安装成功。
- [ ] A2. `apps/desktop/frontend/src/spike/LiveKitAudioSpike.tsx` 存在，能够输入 LiveKit URL/token、连接、发布麦克风、订阅远端音频、展示状态和错误，并支持断开清理。
- [ ] A3. `apps/desktop/frontend/src/App.tsx` 在本任务期间渲染 LiveKit 音频 spike 页面。
- [ ] A4. 自动验证通过：`npm run test:run`。
- [ ] A5. 自动验证通过：`npm run build`。
- [ ] A6. 桌面验证命令 `wails3 dev` 可启动 spike 页面；若因为 HITL 交互未能自动完成，必须记录实际手动执行结果。
- [ ] A7. 两个客户端可以加入同一个测试 LiveKit 房间。
- [ ] A8. A 客户端说话时 B 客户端能听到，B 客户端说话时 A 客户端能听到。
- [ ] A9. `docs/spikes/wails-livekit-audio.md` 记录验证环境、测试步骤、通过结果、失败现象或限制、后续约束，并明确不暴露 token 明文。
- [ ] A10. 若 A7/A8 失败，记录标记为 `Result: fail`，并明确停止后续正式媒体实现。

## Planning Notes

- 本任务是复杂 HITL spike，已提供 `design.md` 和 `implement.md`。
- 本任务完成后仍不能代表正式房间语音功能完成；它只证明 WebView2 + LiveKit 音频路径可用。
- HITL 环境决策已确认：使用公网 LiveKit + 第二客户端。