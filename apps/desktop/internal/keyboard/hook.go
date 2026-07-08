package keyboard

import "strings"

const (
	// PushToTalkEventName is the Wails custom event consumed by the React spike UI.
	PushToTalkEventName = "keyboard:push-to-talk"
	DefaultTargetKey    = "V"
	SourceNative        = "native"
)

// Event is the JSON-serializable keyboard payload sent through the Wails bridge.
type Event struct {
	Key     string `json:"key"`
	Pressed bool   `json:"pressed"`
	Source  string `json:"source"`
}

type transitionTracker struct {
	targetKey string
	pressed   bool
}

func newTransitionTracker(targetKey string) *transitionTracker {
	normalizedTarget := normalizeKey(targetKey)
	if normalizedTarget == "" {
		normalizedTarget = DefaultTargetKey
	}

	return &transitionTracker{targetKey: normalizedTarget}
}

func (t *transitionTracker) apply(key string, pressed bool) (Event, bool) {
	normalizedKey := normalizeKey(key)
	if normalizedKey != t.targetKey {
		return Event{}, false
	}

	if pressed {
		if t.pressed {
			return Event{}, false
		}
		t.pressed = true
		return Event{Key: t.targetKey, Pressed: true, Source: SourceNative}, true
	}

	if !t.pressed {
		return Event{}, false
	}
	t.pressed = false
	return Event{Key: t.targetKey, Pressed: false, Source: SourceNative}, true
}

func normalizeKey(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}
