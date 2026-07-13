# 设置运行时、设备选择与窗口布局设计

## 1. 根因与边界

当前设置页将 `microphone_device` 和 `output_device` 作为自由文本输入，因而不会显示 Windows 当前媒体设备；当前主窗口只设置 `1000×618`，而 CSS shell 使用会增长的 `min-height: 100dvh`，卡片内容高于可用 content area 时会把页面本身撑出 viewport。

`@wailsio/runtime` 对 `/wails/custom.js` 使用 `HEAD` 探测并在缺失时 catch；它是可选 server-mode 脚本，和 `Call.ByID` 的 `/wails/runtime` transport 独立。用户抓包中的 `Save` method ID `2796213707` 返回了 `Settings` JSON，故本 task 不伪造 `custom.js`，而是在成功 Save 后呈现可见状态。

ADR 0007 是硬边界：设备发现和未来实际切换属于 WebView2/LiveKit JS；Go 只存储用户偏好，不加入 native audio stack。

## 2. 设备合同

新增纯前端 `media/devices.ts`：

```text
navigator.mediaDevices
  ├─ enumerateDevices() → 当前可见 audioinput/audiooutput 列表
  └─ getUserMedia({ audio: true }) → 用户触发的标签授权；立即 stop tracks
```

模块返回结构化状态，而不是抛给渲染层：`ready`、`permission-required`、`permission-denied`、`unavailable`。每个选择器始终有 `"" = 跟随系统默认`；枚举值只来自 `MediaDeviceInfo.deviceId`。无 WebView2 媒体 API、拒绝权限、无设备时不得伪造设备条目。

`HTMLMediaElement.prototype.setSinkId` 的存在性标记输出选择能力。当前没有 LiveKit `Room`，所以本 task 只枚举和保存设备偏好；实际 `room.switchActiveDevice` 留给房间媒体接入。输出不可选时 UI 明示系统默认回退，仍不宣称切换完成。

## 3. UI 与窗口合同

`App` 用 device state 替换两个自由文本输入：

- 设备 select 仅包含系统默认和枚举结果；
- `授权并刷新设备` 由用户点击触发权限请求并刷新标签；
- 状态文案用 `role=status`，错误仍用现有 `role=alert`；
- Save 成功显示短暂但可读的 `本地设置已保存` 状态，随后任何编辑清除该状态。

`mainWindowOptions` 提取为可测试函数，使用：初始 `720×720`、最小 `600×640`、最大 `1000×900`、背景 `#F3F6F8`。尺寸与当前 560px settings card 对齐；小于内容时 CSS 只让 card 内部滚动。

CSS shell 改为固定 `height: 100dvh`、`overflow: hidden`、最小尺寸 `0`；card 用 `max-height: 100%` 和 `overflow: auto`。因此 viewport 永不产生纵向滚动条，主操作在 card 的可访问滚动区域内。

## 4. 测试与回归环

1. `media/devices.test.ts`：过滤真实媒体 kind、默认项、授权后重枚举、拒绝/缺失 API、输出支持 flag、track cleanup。
2. `App.test.tsx`：设备输入为 select、刷新行为与权限文案、保存完整 device payload、成功状态、shell overflow class。
3. `main_test.go`：窗口初始/min/max 尺寸与浅色 native background。
4. HITL：真实 `wails3 dev` desktop 中授权设备、保存、重启恢复、最小窗口 resize；记录可选 `custom.js` 404 不改变 Save promise。

## 5. 回滚

删除 `media/devices.ts`、恢复自由文本输入及现有 shell/window values 可回退本 task；`config.Settings` JSON 格式和 Wails service binding 不变。