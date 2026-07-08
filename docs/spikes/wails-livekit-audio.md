# Wails 3 + WebView2 + LiveKit 音频路径 Spike

Result: pass (automated + Windows HITL)

## 当前实现状态

已实现一个隔离的 LiveKit 音频 spike 页面，用于验证 Wails 3 WebView2 中的媒体路径：

- 手动输入公网 LiveKit WSS URL。
- 手动粘贴短期 join token，token 仅保存在本次页面内存状态中。
- 点击“连接并发布麦克风”后创建 LiveKit `Room`，连接房间，并调用 `setMicrophoneEnabled(true)` 发布本地麦克风。
- 订阅远端 `audio` track 后使用 `track.attach()` 创建浏览器音频元素，并挂载到页面专用容器。
- 提供“恢复远端播放”和“断开并清理”操作。
- 事件记录只显示非 secret 状态，不记录 token、API key、API secret、房间 session secret 或音频内容。

## 自动验证结果

本轮自动验证已重新运行并通过：

- `npm --prefix apps/desktop/frontend run test:run`: pass，4 个测试文件、21 个测试通过。
- `npm --prefix apps/desktop/frontend run build`: pass；Vite 因 LiveKit bundle 输出超过 500 kB 给出 chunk-size warning，按本任务约定为非阻塞。
- `go -C apps/desktop test ./...`: pass，无测试文件。
- `cd apps/desktop && wails3 build`: pass，Wails v3.0.0-alpha2.115 构建输出 `apps/desktop/bin/echo.exe`。

## Windows HITL 验证结果

Manual Windows validation completed by the user on 2026-07-07:

| 字段 | 记录 |
| --- | --- |
| Windows 版本 | Windows 11 Pro 10.0.26200 x64 |
| Wails 版本 | `v3.0.0-alpha2.115` |
| WebView2 运行环境 | Wails 3 桌面窗口内 WebView2 |
| LiveKit 服务地址类别 | LiveKit Cloud 公网 WSS 服务；未记录 URL、API key、API secret 或 token 明文 |
| 第二客户端类型 | 另一台设备上的 LiveKit 客户端（用户报告，与 Wails 客户端加入同一 LiveKit Cloud 测试房间）；未记录任何 secret |
| 客户端数量 | 至少 2 个客户端加入同一 LiveKit Cloud 测试房间 |
| token 生成方式摘要 | LiveKit Cloud 测试凭据 / 短期 join token；未记录 token 明文 |
| A 到 B 音频 | pass；用户报告已连接并能听到不同设备的声音 |
| B 到 A 音频 | pass；用户报告已连接并能听到不同设备的声音 |
| 断开清理 | spike 页面提供“断开并清理”；自动测试覆盖 disconnect cleanup，HITL 未报告异常 |
| 失败现象或限制 | 未报告阻断失败；未验证自托管 LiveKit、外部 Nginx 或 TURN |

## 手动验证步骤

1. 从 `apps/desktop` 运行 `wails3 dev`。
2. 确认 Wails 窗口显示“LiveKit 音频路径验证”。
3. 粘贴公网可访问的 LiveKit WSS URL。
4. 粘贴只用于本次测试的短期 join token。
5. 点击“连接并发布麦克风”。
6. 如系统提示麦克风权限，允许本次测试使用麦克风。
7. 使用第二客户端加入同一个 LiveKit 测试房间。
8. A 客户端说话，确认 B 客户端可以听到。
9. B 客户端说话，确认 A 客户端可以听到。
10. 观察页面事件记录中是否出现连接、麦克风发布、远端音频订阅和播放状态。
11. 如远端播放被浏览器策略阻止，点击“恢复远端播放”后重新确认。
12. 点击“断开并清理”，确认房间断开、麦克风释放、远端音频元素清理。
13. 确认未把 token、API secret、room session secret 或音频内容写入文档、日志或提交内容。

## 通过结论

本次 HITL 满足 Issue #8 的媒体路径验证目标：

- Wails 3 WebView2 窗口可以连接公网 LiveKit Cloud WSS 服务。
- 本地麦克风权限可请求且本地 audio track 可发布。
- 至少两个客户端可加入同一个测试 LiveKit 房间。
- 不同设备之间可以听到彼此声音，满足双端语音路径验证。
- 远端音频通过 WebView2 浏览器路径播放，不经过 Go 音频播放管线。
- 记录不包含 token、API key、API secret、room session secret、音频内容或可长期复用凭据。

## 失败 stop rule

如果后续复测出现以下任一环节失败，应改为 `Result: fail`，记录确切失败阶段和复现条件，并暂停正式媒体实现：

- LiveKit JS 无法在 Wails WebView2 中连接。
- WebView2 无法请求或使用麦克风权限。
- 本地麦克风 audio track 无法发布。
- 远端 audio track 无法订阅。
- 远端 audio track 订阅后无法通过 WebView2 播放。
- 双向音频无法成立且排除 token、房间不匹配、第二客户端和临时网络配置问题后仍失败。

失败时不得在本任务内引入 Go 音频采集/播放管线、Electron fallback、服务端混音、TURN、Redis 或正式临时房间功能作为绕过。

## 后续约束

- 该 spike 只验证媒体路径，不代表正式临时房间、邀请码、成员状态、静音同步、业务 WebSocket 或 API token 签发完成。
- 正式实现仍必须保持音频采集、发布、订阅和播放在 WebView2 + LiveKit JS 中。
- Go/Wails 层只负责原生窗口、托盘、设置、日志和按键桥接，不采集、不编码、不混音、不播放房间语音。
- LiveKit Cloud 验证通过不等同于自托管 LiveKit + 外部 Nginx 部署验证通过；后续部署路径仍需按独立任务验证。
- 公开 HITL 记录只能写非 secret 摘要；真实 token 和密钥不得进入仓库。
