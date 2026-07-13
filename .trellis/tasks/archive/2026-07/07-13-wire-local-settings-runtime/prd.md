# 接入本地设置运行时

## 目标

修复 PR #39 review 发现的运行时断链：让 `config.Store` 的本机身份与设置能力在 Wails 桌面应用启动和 React UI 中实际可达。首次启动须生成身份/头像；用户须能保存并在重启后恢复昵称、头像、快捷键、设备、语音模式与音量；自由说话偏好不得自动发送音频。

## 已确认事实与约束

- PR #39 当前只新增了 `config.Store`、手写前端设置类型和语音门控；`main.go` 没有创建/注册设置服务，`App.tsx` 只渲染 `KeyboardSpike`。因此 Issue #17 的运行时验收未覆盖。
- Wails v3 将导出 Go 服务方法注册为 `application.NewService`，并为 React 生成 TypeScript binding；本任务必须使用该路径，不建立前端伪 bridge。
- 本地设置仍由 Wails Go 层在当前 Windows 用户配置目录保存（`docs/adr/0017-local-settings-in-user-config.md:3`）。服务端、账号、云同步和房间历史不属于范围。
- 首次昵称页在本地没有昵称时显示随机头像、昵称输入和继续按钮；昵称不能为空且最多 16 个字符（`docs/ui/client-ui-spec.md:657-675`）。
- `free_talk` 是保存偏好，进入新房间时仍需明确开启；静音/连接/麦克风门控保持 `voiceState.ts` 的规则。

## 需求

### R1：启动与服务注册

桌面启动必须构造默认 `config.Store`、加载一次可用设置，并注册一个只暴露 Load、Save、ResetAvatar 的 Wails service。缺失或损坏文件应沿用 Store 的安全恢复；真实 I/O 错误必须作为启动/调用错误可见，不能伪造已保存状态。

### R2：单一持久化合同

Go `config.Settings` 是序列化与默认值的唯一 owner。React 必须通过 Wails 生成 binding 使用该服务，不能再维护独立的八字段 `LocalSettings` 或重复安全默认值。任何前端转换只能位于该 binding adapter seam。

### R3：首次昵称与恢复界面

React 启动时加载服务设置。昵称为空时显示首次昵称页；继续按钮仅在 1–16 字符昵称时启用，并通过 Save 持久化。已有昵称时显示已恢复的设置状态与可编辑字段。

### R4：可操作的设置

用户可通过同一 Save 路径更新快捷键、麦克风设备值、输出设备值、语音模式偏好和 0–100 整体输出音量；可通过 ResetAvatar 重置头像。UI 保存后的状态必须来自服务返回值。

### R5：语音安全

保存 `free_talk` 不得改变本房间 `freeTalkEnabledInRoom=false` 的拒绝发送结果。既有 `canSendAudio` 门控继续覆盖连接、设备、静音和模式规则。

## 验收标准

- [ ] AC1：新增 App 测试在当前应用的首次无昵称状态下红色，在接线后显示随机头像、昵称输入与禁用的继续按钮；输入有效昵称后调用服务 Save 并显示已保存昵称。
- [ ] AC2：Go service 测试证明 Load、Save、ResetAvatar 只委托给 Store seam，并将 Store 错误返回给调用者。
- [ ] AC3：Wails binding generation 生成 SettingsService，前端生产构建成功导入该 binding；`wails3 build` 成功编译注册后的桌面应用。
- [ ] AC4：重启/重新加载路径恢复服务保存的昵称、设备、音量、快捷键和语音偏好；ResetAvatar 仅改变头像。
- [ ] AC5：昵称为空或超过 16 字符不能保存；保存 `free_talk` 时 `freeTalkEnabledInRoom=false` 仍不允许发送。
- [ ] AC6：无账号、云同步、房间历史、自动加入、头像上传/图库、设备枚举/切换或媒体播放实现。

## 非目标

- 完整首页、临时房间、设置抽屉、设备枚举/实际切换、LiveKit 媒体、托盘或键盘 spike 重构。
- 账号、云端资料、服务端设置、房间历史、自动加入和关闭窗口偏好。

## 未决问题

无。Issue #17、ADR 0017、UI 规范和 PR review 已确定范围；用户已明确要求进行修复。
