//go:build !windows

package keyboard

// Hook is a no-op outside Windows. The MVP validates support only on Windows 10/11 x64.
type Hook struct {
	targetKey string
	onEvent   func(Event)
}

func NewHook(targetKey string, onEvent func(Event)) *Hook {
	normalizedTarget := normalizeKey(targetKey)
	if normalizedTarget == "" {
		normalizedTarget = DefaultTargetKey
	}

	return &Hook{targetKey: normalizedTarget, onEvent: onEvent}
}

func (h *Hook) Start() error {
	return ErrUnsupported
}

func (h *Hook) Stop() {}
