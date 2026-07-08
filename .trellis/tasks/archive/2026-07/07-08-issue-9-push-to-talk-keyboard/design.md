# Design: Issue #9 Push-to-talk Keyboard Spike

## Scope

本设计只覆盖按键说话 press/release spike。目标是建立一个可观察、可测试、可记录的按键事件链路：Windows 原生事件源 → Wails app event → React spike UI → HITL 记录。

## Boundaries

- Go/Wails 层负责按键事件采集和桥接，不负责语音发送或媒体控制。
- React 层负责 spike UI、事件状态推导和测试展示，不负责正式房间或正式设置保存。
- 文档层负责记录自动验证、Windows HITL 和兼容性限制。
- 反作弊限制、管理员权限隔离和游戏自身按键占用是兼容性边界；本任务只记录，不规避。

## Architecture

```text
Windows keyboard input
  ↓
apps/desktop/internal/keyboard Hook
  ↓ Event{key, pressed, source}
apps/desktop/main.go emits Wails event "keyboard:push-to-talk"
  ↓
frontend KeyboardSpike subscribes to Wails runtime event
  ↓
keyboardState reducer computes counts, cycles, warnings
  ↓
visible spike page + docs/spikes/push-to-talk-keyboard.md
```

## Go keyboard package

Files:

- `apps/desktop/internal/keyboard/hook_windows.go`
- `apps/desktop/internal/keyboard/hook_nonwindows.go`
- optional `apps/desktop/internal/keyboard/hook_test.go` for OS-independent state-free helpers if needed.

Public contract:

```go
type Event struct {
    Key     string `json:"key"`
    Pressed bool   `json:"pressed"`
    Source  string `json:"source"`
}

type Hook struct { ... }
func NewHook(targetKey string, onEvent func(Event)) *Hook
func (h *Hook) Start() error
func (h *Hook) Stop()
```

Windows implementation requirements:

- Track only the configured target key for the spike; default `V`.
- Emit `pressed=true` only on transition from up to down; ignore key repeat while already pressed.
- Emit `pressed=false` only on transition from down to up.
- Keep callback non-blocking enough not to stall the hook path; call into app event emission with minimal work.
- `Stop` must unhook and be idempotent.
- If low-level hook setup fails, return an error and let UI show the failure through startup/log/event state where possible.

Non-Windows implementation:

- Compile and return no-op behavior.
- Do not claim support outside Windows.

## Wails bridge

Modify `apps/desktop/main.go`:

- Create the keyboard hook after `app` and `mainWindow` are initialized.
- Emit events to the frontend using a stable event name, e.g. `keyboard:push-to-talk`.
- Start hook before or during app startup and stop it on app shutdown/defer.
- Preserve existing tray and close-to-tray behavior from Issue #7.

If the exact Wails 3 app event API differs during implementation, use the smallest supported event mechanism available in the installed Wails version and keep the frontend subscription isolated in one module.

## Frontend design

Files:

- `apps/desktop/frontend/src/spike/keyboardState.ts`
- `apps/desktop/frontend/src/spike/keyboardState.test.ts`
- `apps/desktop/frontend/src/spike/KeyboardSpike.tsx`
- `apps/desktop/frontend/src/spike/KeyboardSpike.test.tsx`
- `apps/desktop/frontend/src/App.tsx`
- `apps/desktop/frontend/src/App.test.tsx`

State contract:

```ts
type KeyboardEventRecord = {
  key: string
  pressed: boolean
  source: 'native' | 'dom'
  sequence: number
}

type KeyboardSpikeState = {
  targetKey: string
  isPressed: boolean
  downCount: number
  upCount: number
  completedCycles: number
  missingRelease: boolean
  events: KeyboardEventRecord[]
}
```

Rules:

- DOM `keydown` / `keyup` is a fallback/control path only when the WebView is focused; native Wails events are the verification path for game foreground scenarios.
- Repeated down while already pressed must not increment completed cycles.
- A complete cycle is one valid down transition followed by one valid up transition for the target key.
- UI must explain how to run the test：focus app for desktop baseline, then switch to a game and perform 10 press/release cycles.
- UI must show a warning if down count exceeds up count or `isPressed=true` after expected release.

## Documentation design

Create `docs/spikes/push-to-talk-keyboard.md` with:

- `Result: pending Windows HITL` until user validates manual scenarios.
- Automated command results.
- Manual steps.
- Scenario matrix: ordinary desktop, borderless game, fullscreen/exclusive game, administrator game, anti-cheat restricted game.
- Compatibility conclusion and stop rules.

After HITL, update the result honestly:

- `pass` only if required ordinary desktop and borderless game scenarios pass and limitations are explicitly recorded.
- `partial` if core desktop works but game/admin/anti-cheat has material unverified or failed paths.
- `fail` if press/release cannot be reliably observed in the baseline desktop or normal foreground-game path.

## Compatibility and rollback

- If native hook is unstable or breaks build, rollback is limited to files added/modified in this spike and restore App route to prior spike.
- If Wails event bridge API is the issue, keep the keyboard package and frontend reducer tests, document the bridge failure, and stop before claiming HITL pass.
- If Windows security boundary blocks admin or anti-cheat scenarios, document limitation rather than adding privileged auto-elevation or evasion logic.
