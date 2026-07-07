# 实施计划：验证音频设备选择和系统托盘保持运行

## Ordered Checklist

1. 准备分支和任务状态。
   - 从当前最新 `master` 创建 `issue-7-device-tray-validation` 分支。
   - 设置 Trellis task branch/base branch。
   - 运行 `task.py start` 进入 in_progress。

2. 实现前端媒体设备封装。
   - 新增 `apps/desktop/frontend/src/spike/mediaDevices.ts`。
   - 类型化 `MediaDeviceInfo` 投影，避免组件散落读取原始字段。
   - 实现设备枚举、指定麦克风采集、输入电平计算、输出设备支持检测和 `setSinkId` 应用。
   - 清理规则：切换设备或卸载时 stop 旧 tracks、关闭 AudioContext/raf/timer。

3. 实现设备/托盘 spike 页面。
   - 新增 `apps/desktop/frontend/src/spike/DeviceTraySpike.tsx`。
   - 展示：权限状态、麦克风列表、当前输入电平、输出设备列表、输出切换状态、运行计时、托盘手动验证说明。
   - 修改 `apps/desktop/frontend/src/App.tsx` 临时渲染 spike 页面。
   - 不实现正式设置页，不做输出测试音，不展示成员音量。

4. 补充前端测试。
   - 覆盖枚举成功、空设备、权限失败、麦克风切换清理、输入电平边界、`setSinkId` unsupported/supported failure。
   - 保持测试面向要求，不断言私有实现细节。

5. 实现 Wails 托盘和关闭窗口隐藏。
   - 修改 `apps/desktop/main.go`，创建主窗口变量。
   - 注册 `events.Common.WindowClosing` hook：普通关闭隐藏窗口并取消关闭；退出菜单允许真正退出。
   - 创建系统托盘，tooltip 为 `echo`；菜单包含“显示主窗口”和“退出 echo”。
   - 如 API 在非 Windows 编译失败，拆出 `internal/tray` Windows/非 Windows 文件。

6. 记录 spike 结果。
   - 新增 `docs/spikes/device-tray.md`。
   - 初始写入自动化结果结构；运行后填入实际命令结果。
   - 对不能由自动化完成的 Windows/HITL 项标记 `pending HITL`，并列出用户可执行步骤。

7. 验证。
   - `cd apps/desktop/frontend && npm run test:run`
   - `cd apps/desktop/frontend && npm run build`
   - `cd apps/desktop && go test ./...`
   - `cd apps/desktop && wails3 build`
   - 手动/HITL：`cd apps/desktop && wails3 dev`，验证设备、输入电平、窗口 X、托盘恢复、托盘退出、隐藏后媒体状态。

8. Trellis check。
   - 调用 `trellis-check` 对当前 active task 检查。
   - 若失败，修复后重复执行直至全部通过或明确记录外部/HITL 阻塞。

## Validation Matrix

| Requirement | Automated validation | Manual/HITL validation |
| --- | --- | --- |
| R1 麦克风枚举 | Vitest mock enumerateDevices | Windows WebView2 授权后看到真实麦克风 |
| R2 输入音量 | 电平计算单测/组件测试 | 对麦克风发声时音量条变化 |
| R3 麦克风切换 | getUserMedia constraint 和 track cleanup 测试 | 选择不同麦克风后状态更新 |
| R4 输出设备 | `setSinkId` supported/unsupported 测试 | WebView2 暴露时尝试切换；否则记录降级 |
| R5 托盘生命周期 | Go 编译/build | 点击 X 隐藏，托盘显示/退出可用 |
| R6 隐藏不中断 | 前端不在 hide 时主动 cleanup | 隐藏后恢复，页面/媒体状态未重置 |
| R7 文档记录 | 文件存在且包含必需小节 | HITL 结果补全 |
| R8 边界保持 | 搜索排除输出测试音/成员音量/Go audio pipeline | 页面未出现正式设置/成员音量 |

## Risky Files and Rollback Points

- `apps/desktop/main.go`：Wails 生命周期入口。若托盘 hook 导致窗口无法退出，先恢复到单窗口创建，再拆分 tray helper。
- `apps/desktop/frontend/src/App.tsx`：临时路由到 spike。若后续任务需要恢复 bootstrap，回滚此文件即可。
- `apps/desktop/frontend/src/spike/**`：所有 spike 前端代码集中在此，便于删除。
- `docs/spikes/device-tray.md`：记录失败也有价值，不因失败删除。

## Scope Guardrails

- 不新增服务端代码。
- 不新增正式 settings 状态模型或本地持久化。
- 不新增 LiveKit 依赖作为本任务前提。
- 不新增输出测试音或成员音量控件。
- 不新增 Go 音频采集/播放/混音。
