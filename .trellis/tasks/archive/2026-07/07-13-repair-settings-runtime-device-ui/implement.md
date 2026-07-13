# 设置运行时、设备选择与窗口布局实施计划

## 1. 建立红环

1. 新增 `src/media/devices.test.ts`，覆盖真实媒体设备的枚举、权限、失败与输出能力合同；运行 `npm run test:run -- src/media/devices.test.ts`，确认缺少模块时失败。
2. 扩展 `src/App.test.tsx`：设备必须是受控 select、成功 Save 有可见状态、shell 具备 viewport overflow 合同；确认当前实现失败。
3. 新增 `apps/desktop/main_test.go`，先锁定目标 Wails window options；确认当前 inline options 无法测试或不满足目标。

## 2. 实现设备与 UI

1. 实现纯 `media/devices.ts` adapter，WebView API 通过参数注入以便测试；只在显式刷新时调用 `getUserMedia`，完成后停止临时 track。
2. 在 App 中加载初始可见设备、渲染默认项和真实 select，提供授权刷新操作与可操作中文状态；保存仅传 device ID。
3. 维护 Save 成功 notice，编辑或失败时清除；保留现有 Load/Save/ResetAvatar error state 与 Unicode nickname rules。
4. 不添加 `custom.js` placeholder；在任务研究中记录 runtime 的 optional-loader 事实。

## 3. 实现窗口与布局

1. 从 `main` 提取 `mainWindowOptions()`，加 Wails `MinWidth`、`MinHeight`、`MaxWidth`、`MaxHeight` 和浅色 native background。
2. 更新 `App.css`：shell 高度固定到 viewport，禁止页面 overflow；card 有 `max-height` 和内部滚动。保持视觉 token、焦点和缩小窗口可用。

## 4. 验证

1. `go test ./...`（`apps/desktop`）。
2. `npm run test:run`、`npm run build:dev`（`apps/desktop/frontend`）。
3. `wails3 build`（`apps/desktop`）。
4. HITL：`wails3 dev`，授权刷新设备；选择输入/输出、保存、重启并检查恢复；把窗口缩到最小尺寸并确认 body 无纵向滚动；记录 Save promise 和成功状态。
5. 完成独立 `trellis-check` source review；任何 confirmed finding 返回第 2/3 步。

## 5. 风险与回滚

- WebView2 的输出选择可能不支持；只显示系统默认回退，不声称设备已切换。
- 当前已有 Wails dev server 占用 9245；HITL 复用该实例，不能并行启动第二个。
- 该 task 保持本地 JSON schema、SettingsService 方法及 generated binding 完全兼容；移除 adapter/UI/window CSS 改动即可回滚。