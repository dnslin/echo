# 本地匿名身份与设置实施计划

## 目标

实现 Issue #17 的桌面端本机设置存储和可测试语音状态规则：首次启动创建身份与头像、跨启动恢复所有允许字段、头像可重新随机、损坏配置安全回退，以及自由说话偏好不自动发送。

## 实施顺序

### 1. 建立 Go 配置模块与测试

文件：

- 新建 `apps/desktop/internal/config/store.go`
- 新建 `apps/desktop/internal/config/store_test.go`

实现：

1. 定义 `Settings`、默认值和稳定 snake_case JSON 字段。
2. 实现从当前用户 echo 配置目录解析生产路径的构造路径；测试可注入临时设置文件路径。
3. 实现 `Load`：缺失文件生成并保存匿名身份/头像；已存在文件读出后归一化；损坏 JSON 回退为可用新默认值并持久化。
4. 实现 `Save`，使用单一归一化路径保证所有返回/保存设置可用。
5. 实现 `ResetAvatar`，只更新随机头像并保留其余设置。
6. 使用 `crypto/rand` 生成不透明随机标识；不添加账号、网络、数据库或云同步依赖。
7. 写需求驱动测试：首次生成、完整恢复、重置头像、损坏 JSON、非法/缺失安全字段归一化。

验证：

```powershell
cd apps\desktop
go test ./internal/config -v
```

### 2. 建立前端设置与语音状态合同

文件：

- 新建 `apps/desktop/frontend/src/settings/settings.ts`
- 新建 `apps/desktop/frontend/src/settings/settings.test.ts`
- 新建 `apps/desktop/frontend/src/state/voiceState.ts`
- 新建 `apps/desktop/frontend/src/state/voiceState.test.ts`

实现：

1. 定义与 Go 字段完整对应的 `LocalSettings` 和按键说话安全默认值。
2. 为前端默认合同添加测试，防止默认自由说话或错误默认音量/快捷键。
3. 定义 `VoiceMode`、`VoiceStateInput` 和纯函数 `canSendAudio`。
4. 测试按键说话、静音优先级、连接/麦克风不可用、自由说话本房间显式开启和保存自由说话偏好但未显式开启的拒绝路径。
5. 不修改当前 spike `App.tsx`，不提前实现首次昵称页、设置抽屉、Wails bridge、设备切换或媒体控制。

验证：

```powershell
cd apps\desktop\frontend
npm run test:run -- src/settings/settings.test.ts src/state/voiceState.test.ts
```

### 3. 集成审查与验证

1. 确认所有持久化字段都由 Go 模块保存，前端合同没有新增与产品冲突的字段。
2. 复查损坏配置路径不会 panic，且回退后的自由说话不会被 `canSendAudio` 自动发送。
3. 运行桌面 Go 回归与前端测试：

```powershell
cd apps\desktop
go test ./...
cd frontend
npm run test:run
```

4. 运行 `trellis-check` 审查任务 PRD、设计、ADR、UI 合同、实现和测试结果；所有确认问题回到本计划对应步骤修复后再复检。

## 风险与回滚

- Windows 用户配置目录不可写时，Store 必须返回实际 I/O error，不能伪造已保存状态；测试不访问真实目录。
- 真实 WebView2 设备切换和媒体发送是后续任务的 HITL 风险，不以 mock 替代本任务的持久化与状态验收。
- 无数据库、API、WebSocket 或配置格式迁移。若必须回滚，只需还原新增本机模块/前端状态文件，并由用户删除新生成的本地设置文件。

## 激活前检查

- [x] Issue #17、产品 PRD、技术设计、实施计划、领域词汇、相关 ADR 和 UI 规范已读取。
- [x] 本任务 `prd.md`、`design.md`、`implement.md` 和两份非示例上下文清单已建立。
- [x] 需求无未决产品问题；用户已要求直接实施和独立质量检查。
- [ ] 在新分支记录任务分支与基础分支。
- [ ] 加载 `trellis-before-dev` 指定的桌面层规范和工具知识。
- [ ] `task.py start` 激活任务后委派实施。