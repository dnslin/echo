# Wails keyboard event ordering research

## Source checked

- `apps/desktop/main.go` emits each native `keyboard.Event` with `app.Event.Emit(keyboard.PushToTalkEventName, event)`.
- Wails v3 `pkg/application/events.go` (`github.com/wailsapp/wails/v3@v3.0.0-alpha2.115`) implements `EventProcessor.Emit` by starting separate goroutines for Go listeners and window dispatch:

```go
go func() {
    defer handlePanic()
    e.dispatchEventToListeners(thisEvent)
}()
go func() {
    defer handlePanic()
    e.dispatchEventToWindows(thisEvent)
}()
```

## Ground truth

The Windows hook callback observes key transitions in OS message order, and the Go channel preserves enqueue order inside the hook. Wails `Emit` does not provide a synchronous per-event delivery barrier to the WebView. Therefore frontend code cannot infer native ordering from arrival order alone.

## Design implication

Native keyboard payloads need a monotonically increasing sequence number. The frontend spike must buffer/replay native events by sequence so a delayed `down` followed by an earlier-arriving `up` is still applied as `down -> up`. DOM fallback should remain separate from native state because it can observe the same physical key while echo is focused.
