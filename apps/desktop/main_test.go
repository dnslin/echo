package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestMainWindowOptionsConstrainSettingsLayout(t *testing.T) {
	options := mainWindowOptions()

	if options.Width != 720 || options.Height != 720 {
		t.Fatalf("initial size = %dx%d, want 720x720", options.Width, options.Height)
	}
	if options.MinWidth != 600 || options.MinHeight != 640 {
		t.Fatalf("minimum size = %dx%d, want 600x640", options.MinWidth, options.MinHeight)
	}
	if options.MaxWidth != 1000 || options.MaxHeight != 900 {
		t.Fatalf("maximum size = %dx%d, want 1000x900", options.MaxWidth, options.MaxHeight)
	}
	if options.BackgroundColour != application.NewRGB(243, 246, 248) {
		t.Fatalf("background colour = %#v, want light app canvas", options.BackgroundColour)
	}
}
