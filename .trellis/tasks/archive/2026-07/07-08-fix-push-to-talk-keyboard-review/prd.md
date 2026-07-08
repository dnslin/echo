# Fix push-to-talk keyboard review findings

## Goal

修复 issue #9 按键说话 keyboard spike code review 中按 P1 → P3 排序的缺陷，使 spike 能可信地区分 Windows native hook 与 WebView DOM fallback，并能把 hook 安装失败、事件乱序和 HITL 操作边界明确暴露给测试者。

## Requirements

- P1：native press/release 不能依赖 Wails 事件到达顺序；即使 WebView 先收到 release 再收到 down，也不能把一次物理按下/松开留成“按下未释放”。
- P1：native Wails 事件与 DOM fallback 必须分开计数、分开状态、分开事件日志；同一次物理按键在 echo 聚焦时不能被合并成一个模糊状态，也不能让 DOM fallback 掩盖 native 丢失 release。
- P2：keyboard hook 安装结果必须暴露给 spike UI，至少区分 `enabled`、`disabled` 和非 Windows `unsupported`，并在失败时显示错误原因。
- P2：Windows hook `Stop` 必须尽最大可能让 hook 线程退出；不能只 unhook 后留下阻塞在 `GetMessageW` 的 locked OS thread。
- P2：HITL 手动说明必须消除累计计数歧义；普通桌面对照与游戏前台验证必须各自有明确重置/基线步骤。
- P3：归档后的 Trellis manifest 不得继续引用归档前 research 路径。
- P3：frontend code-spec 中的 Go 事件常量示例必须与实际 `keyboard` 包导出名一致。

## Acceptance Criteria

- [ ] 前端状态测试覆盖 native 乱序到达：收到 seq=2 release 后收到 seq=1 down，最终 native 完整循环为 1，且不显示缺失 release。
- [ ] 前端状态/UI 测试覆盖 native 与 DOM 独立计数：同一轮 DOM down/up 和 native down/up 后，两条路径各计 1 个完整循环，不再只有一个总计数。
- [ ] 前端状态/UI 测试覆盖 hook disabled 状态：收到失败状态后 UI 明确显示 native hook 不可用和错误原因，DOM fallback 仍标为仅 WebView 聚焦对照。
- [ ] Go 测试覆盖 native event sequence 单调递增，确保 hook 派发给 Wails 前已经具备重排所需字段。
- [ ] Go 测试或可执行验证覆盖 `Stop` 退出路径的可测试部分；Windows-only 线程退出不可完全自动化时，必须在实现说明中明确剩余 HITL 边界。
- [ ] HITL 文案包含每轮前点击“重置统计”或记录基线的步骤，游戏前台验证读取 native 路径计数而不是累计总数。
- [ ] `python ./.trellis/scripts/task.py validate .trellis/tasks/archive/2026-07/07-08-issue-9-push-to-talk-keyboard` 通过，或若归档验证脚本本身有外部限制，输出中不再包含旧 research 路径缺失。
- [ ] `cd apps/desktop && go test ./...` 通过。
- [ ] `cd apps/desktop/frontend && npm run test:run` 通过。
- [ ] `cd apps/desktop/frontend && npm run build` 通过。

## Diagnose feedback loop

- 快速自动 loop：Go package tests + Vitest tests，重点锁住 native event sequence、frontend reorder buffer、source-isolated counters、hook status UI。
- HITL loop：运行 `wails3 dev` 后按 UI 指引执行普通桌面对照与游戏前台 10 次按下/松开；记录 native 与 DOM 两条路径的计数和 hook 状态。

## Ranked hypotheses

1. 如果 Wails `Emit` 的异步窗口派发是 P1 乱序根因，那么给 native payload 加 sequence 并在前端按 sequence 重排会让 release-before-down 的测试消失。
2. 如果 DOM 与 native 共用状态是 P1 误判根因，那么按 source 拆分状态后，同一次物理按键在聚焦 echo 时只会分别增加 DOM/native 计数，不会互相掩盖。
3. 如果 hook 安装失败只写日志是 P2 误判根因，那么状态握手事件能让 UI 在 native hook 不可用时直接显示 disabled，而不是让 DOM fallback 伪装成 native 通过。
4. 如果 `Stop` 线程泄漏来自 message queue 未唤醒，那么在线程初始化时确保 message queue 创建、停止时重复 `WM_QUIT` 并在超时前 unhook，能降低阻塞在 `GetMessageW` 的概率。
5. 如果 HITL 指引失败来自累计计数语义，那么提供重置按钮和每轮独立路径计数会消除“第二轮看到 20 还是 10”的歧义。

## Notes

- 不实现提权、反作弊绕过或检测规避；管理员权限游戏/反作弊限制只作为兼容性边界记录。
- 不改变 MVP 默认快捷键、语音模式产品语义或 LiveKit 媒体路径。
