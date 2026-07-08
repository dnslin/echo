//go:build windows

package keyboard

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	whKeyboardLL = 13
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	wmQuit       = 0x0012

	hookEventBufferSize = 64
	stopWaitTimeout     = 2 * time.Second
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetWindowsHookExW   = user32.NewProc("SetWindowsHookExW")
	procCallNextHookEx      = user32.NewProc("CallNextHookEx")
	procUnhookWindowsHookEx = user32.NewProc("UnhookWindowsHookEx")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	procGetCurrentThreadID  = kernel32.NewProc("GetCurrentThreadId")
	procGetModuleHandleW    = kernel32.NewProc("GetModuleHandleW")
)

type keyboardLLHookStruct struct {
	vkCode      uint32
	scanCode    uint32
	flags       uint32
	time        uint32
	dwExtraInfo uintptr
}

type point struct {
	x int32
	y int32
}

type message struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

// Hook captures Windows low-level keyboard press/release transitions for one target key.
type Hook struct {
	mu        sync.Mutex
	targetKey string
	targetVK  uint32
	onEvent   func(Event)
	tracker   *transitionTracker
	callback  uintptr
	hook      windows.Handle
	threadID  uint32
	events    chan Event
	done      chan struct{}
	running   bool
}

func NewHook(targetKey string, onEvent func(Event)) *Hook {
	normalizedTarget := normalizeKey(targetKey)
	if normalizedTarget == "" {
		normalizedTarget = DefaultTargetKey
	}

	return &Hook{
		targetKey: normalizedTarget,
		targetVK:  virtualKeyCode(normalizedTarget),
		onEvent:   onEvent,
		tracker:   newTransitionTracker(normalizedTarget),
	}
}

func (h *Hook) Start() error {
	if h == nil {
		return fmt.Errorf("keyboard hook is nil")
	}
	if h.targetVK == 0 {
		return fmt.Errorf("unsupported keyboard target key %q", h.targetKey)
	}

	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return nil
	}
	done := make(chan struct{})
	events := make(chan Event, hookEventBufferSize)
	h.done = done
	h.events = events
	h.running = true
	started := make(chan error, 1)
	h.mu.Unlock()

	go h.run(started, done, events)

	if err := <-started; err != nil {
		<-done
		h.mu.Lock()
		h.running = false
		h.done = nil
		h.events = nil
		h.mu.Unlock()
		return err
	}

	return nil
}

func (h *Hook) Stop() {
	if h == nil {
		return
	}

	h.mu.Lock()
	if !h.running {
		h.mu.Unlock()
		return
	}
	threadID := h.threadID
	done := h.done
	h.running = false
	h.mu.Unlock()

	if threadID != 0 {
		posted, _, _ := procPostThreadMessageW.Call(uintptr(threadID), wmQuit, 0, 0)
		if posted == 0 {
			h.unhook()
		}
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(stopWaitTimeout):
		}
	}

	h.mu.Lock()
	h.done = nil
	h.events = nil
	h.mu.Unlock()
}

func (h *Hook) run(started chan<- error, done chan struct{}, events chan Event) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(done)
	dispatcherDone := make(chan struct{})
	go h.dispatchEvents(events, dispatcherDone)
	defer func() {
		close(events)
		<-dispatcherDone
	}()

	h.mu.Lock()
	h.threadID = currentThreadID()
	h.callback = syscall.NewCallback(h.handleKeyboard)
	h.mu.Unlock()

	module, _, moduleErr := procGetModuleHandleW.Call(0)
	if module == 0 {
		started <- windowsCallError("get module handle for keyboard hook", moduleErr)
		return
	}

	hookHandle, _, callErr := procSetWindowsHookExW.Call(
		whKeyboardLL,
		h.callback,
		module,
		0,
	)
	if hookHandle == 0 {
		started <- windowsCallError("install low-level keyboard hook", callErr)
		return
	}

	h.mu.Lock()
	h.hook = windows.Handle(hookHandle)
	h.mu.Unlock()
	defer h.unhook()

	started <- nil
	h.messageLoop()
}

func (h *Hook) handleKeyboard(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 && lParam != 0 {
		pressed, isTransition := keyTransitionFromMessage(uint32(wParam))
		if isTransition {
			keyboardEvent := (*keyboardLLHookStruct)(unsafe.Pointer(lParam))
			if keyboardEvent.vkCode == h.targetVK {
				if event, ok := h.tracker.apply(h.targetKey, pressed); ok {
					h.enqueueEvent(event)
				}
			}
		}
	}

	result, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return result
}

func (h *Hook) enqueueEvent(event Event) {
	h.mu.Lock()
	events := h.events
	h.mu.Unlock()
	if events == nil {
		return
	}

	// Keep the Windows hook callback non-blocking; the buffer covers the 10-cycle HITL scenario.
	select {
	case events <- event:
	default:
	}
}

func (h *Hook) dispatchEvents(events <-chan Event, done chan<- struct{}) {
	defer close(done)
	for event := range events {
		if h.onEvent != nil {
			h.onEvent(event)
		}
	}
}

func (h *Hook) messageLoop() {
	var msg message
	for {
		result, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(result) == -1 || result == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func (h *Hook) unhook() {
	h.mu.Lock()
	hookHandle := h.hook
	h.hook = 0
	h.threadID = 0
	h.mu.Unlock()

	if hookHandle != 0 {
		procUnhookWindowsHookEx.Call(uintptr(hookHandle))
	}
}

func keyTransitionFromMessage(message uint32) (pressed bool, ok bool) {
	switch message {
	case wmKeyDown, wmSysKeyDown:
		return true, true
	case wmKeyUp, wmSysKeyUp:
		return false, true
	default:
		return false, false
	}
}

func virtualKeyCode(key string) uint32 {
	if len(key) != 1 {
		return 0
	}
	character := key[0]
	if character >= 'A' && character <= 'Z' {
		return uint32(character)
	}
	if character >= '0' && character <= '9' {
		return uint32(character)
	}
	return 0
}

func currentThreadID() uint32 {
	threadID, _, _ := procGetCurrentThreadID.Call()
	return uint32(threadID)
}

func windowsCallError(action string, err error) error {
	if err == nil || err == syscall.Errno(0) {
		return fmt.Errorf("%s failed", action)
	}
	return fmt.Errorf("%s: %w", action, err)
}
