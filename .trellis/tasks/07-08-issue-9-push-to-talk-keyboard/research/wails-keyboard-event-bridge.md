# Wails keyboard event bridge research

## Question

How should Issue #9 bridge native Windows keyboard press/release events from Go/Wails into the React spike UI?

## Evidence

- Local Wails v3 example `github.com/wailsapp/wails/v3@v3.0.0-alpha2.115/examples/badge-custom/main.go` emits custom events with `app.Event.Emit("time", now)` from Go.
- Local frontend runtime types `apps/desktop/frontend/node_modules/@wailsio/runtime/types/events.d.ts` expose `Events.On(eventName, callback): () => void` and `Events.Emit(name, data)`. The event object carries `event.data`.
- Local runtime implementation `@wailsio/runtime/dist/events.js` dispatches custom events by name and calls registered listeners.
- Context7 `/websites/v3_wails_io` confirms Wails v3 custom events: Go backend can call `app.Event.Emit("data-updated", map[string]interface{}{...})`; frontend can listen via `import { Events } from "@wailsio/runtime"` and `Events.On("data-updated", (event) => { ... })`.
- Context7 also documents `WebviewWindow.EmitEvent(name, data...)` for window-targeted events, but the current app has one main window, so app-wide `app.Event.Emit` is sufficient and matches local Wails examples.

## Decision

Use a stable custom event name `keyboard:push-to-talk`. The Windows keyboard hook emits `keyboard.Event{Key:"V", Pressed:true/false, Source:"native"}`. `apps/desktop/main.go` calls `app.Event.Emit("keyboard:push-to-talk", event)` from the hook callback. The frontend `KeyboardSpike` imports `Events` from `@wailsio/runtime` and subscribes with `Events.On("keyboard:push-to-talk", handler)`.

## Constraints

- Keep DOM `keydown`/`keyup` only as a focused-window fallback/control path; native events are the game-foreground validation path.
- Do not use frontend event receipt as proof of game compatibility until Windows HITL records ordinary desktop and game foreground scenarios.
- Do not add formal voice sending, shortcut editor, privilege escalation, or anti-cheat bypass behavior.

## Implementation notes

- The event payload should be a plain JSON-serializable struct.
- The frontend should tolerate malformed or unknown event data and ignore non-target keys.
- The Wails event bridge is supported by both local source and current Wails v3 docs; no extra binding generation is required for arbitrary string event names.
