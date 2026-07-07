# Device and Tray Spike

Result: pass (automated + Windows HITL)

## Validated

Automated validation completed:

- Frontend media wrapper tests cover audio device enumeration, empty device lists, microphone request constraints, input level calculation bounds, Web Audio cleanup, input-level unsupported reporting, and output `setSinkId` supported/unsupported paths.
- Component tests cover empty device states, permission failure handling, microphone switching cleanup, stale microphone request suppression, meter-creation failure cleanup, stale device selection refresh, empty default output sink IDs, and applying the selected output device from the page.
- Desktop Go entrypoint wires Wails system tray and cancels ordinary window close by hiding the main window.
- PR #29 closes Issue #7 after automated validation and Windows HITL validation both passed.

Manual Windows validation completed by the user on 2026-07-07:

- Request microphone permission in Wails/WebView2 and confirm real `audioinput` labels appear: pass.
- Speak into the selected microphone and confirm the input level meter changes: pass.
- Switch between available microphones and confirm the page keeps running: pass.
- Refresh after device changes and confirm stale selections recover to available devices: pass.
- Click window X and confirm echo remains in the system tray: pass.
- Use tray “显示主窗口” and confirm the runtime counter/media state did not reset: pass.
- Use tray “退出 echo” and confirm the process exits: pass.

## Output device result

Automated branch coverage is implemented for both outcomes:

- Supported: if `HTMLMediaElement.setSinkId` exists, the spike applies the selected sink ID and reports success or failure.
- Unsupported: if `setSinkId` is absent, the spike reports `当前 WebView2 不支持指定输出设备，已跟随系统默认输出设备。`

HITL output-device validation passed on Windows WebView2. The spike path produced an acceptable supported success or documented fallback result without crashing.

## Tray result

Implementation path:

- The main Wails window registers `events.Common.WindowClosing`.
- Ordinary close calls `Hide()` and cancels the close event.
- The system tray menu contains `显示主窗口` and `退出 echo`.
- `显示主窗口` calls `Show()` and `Focus()`.
- `退出 echo` allows quit and calls `app.Quit()`.

Manual tray behavior passed Windows HITL verification: close hides to tray, tray restore keeps runtime/media state, and tray quit exits the process.

## Fallback and follow-up constraints

- If output device switching is unsupported on a later target machine, v0.1 must follow the system default output device and show a visible product note before shipping.
- Do not add a Go audio capture, playback, mixing, or output-test pipeline.
- Do not turn this spike into the formal settings page, formal LiveKit room flow, output test tone, member volume control, or server room implementation.

## Command results

Automated results from this implementation pass:

- `npm run test:run` in `apps/desktop/frontend`: pass, including code-review regression coverage for async microphone requests, meter cleanup, stale device refresh, empty sink IDs, and input-level unsupported reporting.
- `npm run build` in `apps/desktop/frontend`: pass.
- `go test ./...` in `apps/desktop`: pass, including after removing generated `frontend/dist` to verify fallback assets.
- `wails3 build` in `apps/desktop`: pass; produced `apps/desktop/bin/echo.exe`.

HITL/manual Windows checks passed for real microphone labels, live input-level movement, microphone switching, output sink behavior or fallback in WebView2, tray restore, tray quit, and hidden-window media persistence.
