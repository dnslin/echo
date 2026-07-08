package keyboard

import (
	"errors"
	"strings"
)

const (
	// PushToTalkEventName is the Wails custom event consumed by the React spike UI.
	PushToTalkEventName = "keyboard:push-to-talk"
	// HookStatusEventName reports whether the native keyboard hook is available.
	HookStatusEventName = "keyboard:hook-status"
	// HookStatusRequestEventName lets the frontend request the current hook status after mount.
	HookStatusRequestEventName = "keyboard:hook-status-request"

	DefaultTargetKey = "V"
	SourceNative     = "native"

	HookStatusEnabled     = "enabled"
	HookStatusDisabled    = "disabled"
	HookStatusUnsupported = "unsupported"
)

var ErrUnsupported = errors.New("keyboard hook unsupported on this platform")

// Event is the JSON-serializable keyboard payload sent through the Wails bridge.
type Event struct {
	Key      string `json:"key"`
	Pressed  bool   `json:"pressed"`
	Source   string `json:"source"`
	Sequence uint64 `json:"sequence,omitempty"`
}

// HookStatus is the JSON-serializable native hook status payload sent to the spike UI.
type HookStatus struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type transitionTracker struct {
	targetKey string
	pressed   bool
	sequence  uint64
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
		return Event{Key: t.targetKey, Pressed: true, Source: SourceNative, Sequence: t.nextSequence()}, true
	}

	if !t.pressed {
		return Event{}, false
	}
	t.pressed = false
	return Event{Key: t.targetKey, Pressed: false, Source: SourceNative, Sequence: t.nextSequence()}, true
}

func (t *transitionTracker) nextSequence() uint64 {
	t.sequence += 1
	return t.sequence
}

func requestThreadQuit(threadID uint32, post func(uint32) bool, unhook func()) bool {
	if threadID == 0 {
		return false
	}
	if post(threadID) {
		return true
	}
	if unhook != nil {
		unhook()
	}
	return post(threadID)
}

func normalizeKey(key string) string {
	return strings.ToUpper(strings.TrimSpace(key))
}
