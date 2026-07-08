package main

import (
	"errors"
	"testing"

	"echo/apps/desktop/internal/keyboard"
)

func TestHookStatusFromErrorReportsUnsupportedPlatform(t *testing.T) {
	status := hookStatusFromError(keyboard.ErrUnsupported)

	if status.Status != keyboard.HookStatusUnsupported {
		t.Fatalf("unexpected status: got %q want %q", status.Status, keyboard.HookStatusUnsupported)
	}
	if status.Message == "" {
		t.Fatal("expected unsupported status to include an error message")
	}
}

func TestHookStatusFromErrorReportsDisabledFailure(t *testing.T) {
	status := hookStatusFromError(errors.New("install low-level keyboard hook: access denied"))

	if status.Status != keyboard.HookStatusDisabled {
		t.Fatalf("unexpected status: got %q want %q", status.Status, keyboard.HookStatusDisabled)
	}
	if status.Message != "install low-level keyboard hook: access denied" {
		t.Fatalf("unexpected message: %q", status.Message)
	}
}
