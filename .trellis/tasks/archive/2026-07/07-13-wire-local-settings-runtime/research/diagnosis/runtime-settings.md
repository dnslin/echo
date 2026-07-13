# 运行时设置接线诊断

## 问题

PR #39 的 `config.Store` 单元测试通过，但运行中的桌面应用从不构造 Store、注册 service 或调用其方法。`main.go` 只注册键盘事件，`App.tsx` 只渲染 KeyboardSpike。

## Phase 1：候选反馈环

在任务激活后，新增/替换 App 测试使其断言：首次无昵称状态显示昵称输入和禁用继续按钮；输入合法昵称后调用 settings service 的 Save 并显示服务返回的持久化状态。

命令：

```powershell
cd apps\desktop\frontend
npm run test:run -- src/App.test.tsx
```

该测试在当前实现必定失败：根组件没有昵称表单、没有 service Load/Save 调用。它是确定、秒级、无人值守且直接覆盖 review 报告的“运行应用没有设置路径”症状。实际 red 输出在执行阶段追加到本文件。

## 初始假设（待红色反馈环后验证）

1. **未接线（最高）**：若 main 和 App 是根因，首次昵称 UI 测试将失败，且它们没有 config/service imports。
2. **Wails binding 路径错误**：若 service 注册但生成路径/模型猜错，`wails3 generate bindings -ts -i` 或前端 build 将失败。
3. **状态更新没有返回持久值**：若 Save 不重新读取 Store，归一化值或新头像不会反映到 UI；service 测试将失败。
4. **持久化偏好越权为会话许可**：若 React 从 free_talk 偏好设置 in-room enabled，voice gate 测试将错误返回 true。

## 排除的 review 候选

`os.Rename` 覆盖已有文件在当前 Windows 工具链中不是根因：`TestResetAvatarChangesOnlyAvatarAndPersists` 已执行 Save → ResetAvatar → Reload 并通过。