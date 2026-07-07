# 技术设计：验证音频设备选择和系统托盘保持运行

## First Principles Analysis

### 1. Challenge Assumptions

- 假设 A：必须先有正式 LiveKit 房间才能验证设备选择。未验证；设备枚举、麦克风采集、输入电平和 `setSinkId` 支持性可直接由 WebView2 浏览器 API 验证。
- 假设 B：输出设备不可切换时应在 Go 层实现播放。错误；ADR 和 design 明确禁止新增 Go 音频管线。
- 假设 C：关闭窗口进托盘只是 UI 行为。错误；关键事实是 WebView 是否被销毁，媒体 stream 是否被主动停止。
- 假设 D：应顺手做正式设置页。错误；Issue 明确是 spike，且边界排除正式设置页。
- 假设 E：输出测试音是验证切换的必要条件。错误；Issue 明确排除输出设备测试音，本任务只验证 API 支持性和记录降级。

### 2. Decompose to Bedrock Truths

- WebView2 中的 `navigator.mediaDevices.enumerateDevices/getUserMedia` 是麦克风枚举和采集的实际能力来源；没有权限时 label 可能为空。
- 输入音量条只需要本地 `MediaStream` + Web Audio 采样，不需要 LiveKit、服务端或回放。
- 输出设备选择在浏览器内只有 `HTMLMediaElement.setSinkId` 可用时才能成立；没有该 API 时无法从前端可靠指定输出设备。
- 关闭按钮如果销毁 WebView，JS runtime 和媒体 stream 会被释放；如果只是 `Hide()`，进程和 WebView 有机会继续存在。
- Wails v3 当前依赖提供 `Window.Hide()`、`Window.Show()`、`SystemTray`、菜单和可取消的 `events.Common.WindowClosing` hook。
- MVP 架构禁止新增 Electron 回退或 Go 音频采集/播放管线。

### 3. Rebuild from Ground Up

1. 为设备验证建立独立前端 spike 页面，避免污染正式 UI。
2. 把浏览器媒体能力封装为小模块：枚举设备、请求指定麦克风、计算电平、尝试 `setSinkId`。
3. 组件只消费这些封装，展示状态和错误；测试 mock 浏览器 API 覆盖边界。
4. Go 入口保留单 WebView；在窗口关闭事件中 `Cancel()` 默认关闭并 `Hide()` 窗口。
5. 创建系统托盘和菜单：显示窗口调用 `Show().Focus()`，退出调用 app quit 路径。
6. 文档记录实际 Windows/HITL 结果；输出设备不支持时只记录降级，不加第二套音频。

### 4. Contrast with Convention

常规思路会直接做设置页或等 LiveKit 正式房间完成后再测设备。这里更基础的约束是：浏览器 API 和 Wails 生命周期是否可行。如果这些能力失败，正式页面再完整也没有价值；因此先做可回滚 spike，并把不可验证项写入 `docs/spikes/device-tray.md`。

### 5. Conclusion

最小但完整的机制是“前端设备 spike + Go/Wails 托盘生命周期 + 结果文档 + 自动化边界测试”。这不是正式产品功能，但必须真实触达 WebView2、mediaDevices、Web Audio、setSinkId 和 Wails 托盘/关闭事件。

## Architecture and Boundaries

### Frontend Boundary

新增 `apps/desktop/frontend/src/spike/`：

- `mediaDevices.ts`：拥有浏览器媒体设备契约。
  - `listMediaDevices()`：返回 audioinput/audiooutput。
  - `requestMicrophone(deviceId?: string)`：请求当前麦克风 stream。
  - `createLevelMeter(stream, onLevel)`：用 Web Audio 计算 0-100 电平，并返回 cleanup。
  - `canSelectOutputDevice()` / `applyOutputDevice(audio, sinkId)`：封装 `setSinkId` 支持性和错误。
- `DeviceTraySpike.tsx`：页面状态、设备选择、权限请求、输入电平展示、输出设备尝试、运行计时。
- `DeviceTraySpike.test.tsx` 和/或 `mediaDevices.test.ts`：覆盖 PRD 的 T1-T7。

主 `App.tsx` 临时渲染 spike 页面。保持中文短文案，不引入正式设置抽屉。

### Go/Wails Boundary

修改 `apps/desktop/main.go`：

- 创建窗口并保留 `mainWindow` 变量。
- 注册 `events.Common.WindowClosing` hook：如果不是托盘“退出 echo”触发，则 `mainWindow.Hide()` 并 `event.Cancel()`。
- 创建系统托盘，设置 tooltip/menu。
- 托盘菜单：
  - “显示主窗口”：`mainWindow.Show().Focus()`。
  - “退出 echo”：设置允许退出标记后调用应用退出/窗口关闭路径。

不在 Go 层读取或控制音频设备。

### Documentation Boundary

新增 `docs/spikes/device-tray.md`。自动化无法替代 Windows 托盘和真实音频设备验证；文档必须区分：

- automated result：单元测试、构建、编译结果；
- HITL result：Windows 上手动确认的设备/托盘表现；
- output fallback：不支持时的产品降级。

## Data Flow

```text
用户点击“请求麦克风权限”
  -> DeviceTraySpike
  -> requestMicrophone(deviceId)
  -> WebView2 getUserMedia
  -> MediaStream
  -> createLevelMeter
  -> UI LevelMeter
```

```text
用户选择输出设备
  -> DeviceTraySpike
  -> canSelectOutputDevice
  -> applyOutputDevice(audioElement, deviceId)
  -> setSinkId supported: 状态=成功/失败
  -> unsupported: 状态=跟随系统默认输出设备
```

```text
用户点击窗口 X
  -> Wails WindowClosing hook
  -> event.Cancel()
  -> mainWindow.Hide()
  -> WebView/JS 不被主动销毁
  -> 托盘“显示主窗口” mainWindow.Show().Focus()
```

## Compatibility and Rollback

- Windows 10/11 x64 是唯一目标平台。
- 非 Windows 环境的 Go 编译不能因托盘 spike 破坏；如 Wails 托盘 API 跨平台可编译则直接使用，否则用构建标签拆分 Windows/非 Windows 文件。
- 前端使用现有 React/Vite/Vitest，不新增 Redux/Zustand。
- 不添加 LiveKit 依赖，除非实现阶段发现 #8 已引入且当前锁文件已包含；本任务不以 LiveKit 为验收前提。
- 回滚点：还原 `App.tsx`、删除 `src/spike/**`、还原 `main.go` 托盘 hook、删除 `docs/spikes/device-tray.md`。

## Risks

- WebView2 可能不暴露 `audiooutput` 或 `setSinkId`；风险处理是记录降级，不实现 Go 播放。
- Windows 隐藏窗口后 WebView2 对媒体/定时器可能有节流；需要 HITL 记录实际表现。
- Wails v3 alpha API 可能在 build 与 dev 下行为不同；必须运行 `go test`、前端测试/build、尽量运行 `wails3 build`。
