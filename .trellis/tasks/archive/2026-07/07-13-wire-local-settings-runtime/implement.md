# 本地设置运行时接线实施计划

## 诊断顺序

1. 将 `App.test.tsx` 改为断言首次昵称体验和服务调用，运行 `npm run test:run -- src/App.test.tsx`，确认当前 KeyboardSpike-only App 红色失败。
2. 展示并按“未接线、错误桥接、错误持久化/状态更新”的优先级检查假设；每次只改一个边界。
3. 用 Go fake-Store service 测试和 React binding-mock App 测试锁定回归。

## 实施步骤

### 1. 接入 Go service

- 新建 `apps/desktop/internal/app/settings_service.go` 与测试。
- 定义窄 Store interface；实现 Load、Save、ResetAvatar 的透明服务行为。
- 修改 `apps/desktop/main.go`：创建默认 Store，首次 Load，注册 service。
- 运行 `go test ./internal/app ./internal/config -v`。

### 2. 生成并消费 Wails binding

- 运行 `wails3 generate bindings -ts -i`，检查生成 `SettingsService` 与 `config.Settings` 模型类型。
- 删除手写八字段 `src/settings/settings.ts` 合同；让 React 通过生成 binding/模型消费设置。
- 不手写或修改 generated binding 文件。

### 3. 实现最小运行时设置 UI

- 新建专注的 settings runtime adapter/screen，替换 `App` 的 KeyboardSpike-only 根内容。
- mount 时 Load；加载失败显示可操作错误。
- 空昵称显示规范的昵称输入、头像和继续操作；1–16 字符才可保存。
- 已有昵称显示可保存的快捷键、设备、模式、0–100 音量及重置头像操作。
- 保留 `voiceState` 的纯门控；它不能从偏好推导当前房间的自由说话开启。

### 4. 验证

- App 测试覆盖首次加载、昵称验证/保存、恢复显示、头像重置、加载或保存错误、保存自由说话不自动发送。
- 运行 `go test ./...`（`apps/desktop`）、`npm run build:dev`、`npm run test:run`（frontend）、`wails3 build`（desktop）。
- 使用 `trellis-check` 独立审查 PRD、UI/ADR、生成 binding、测试与范围；修复所有确认 findings 后再更新 PR #39。

## 风险

- 生成 binding 是源代码事实；模型字段命名须以生成输出为准，不能猜测。
- Wails build 对 WebView/Windows 环境敏感；unit/build 失败须先区分 generated binding、Go service 注册和环境依赖。
- 本任务不更改 JSON 格式或已有用户设置，不需要迁移。