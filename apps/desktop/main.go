package main

import (
	"errors"
	"log"
	"sync/atomic"

	"echo/apps/desktop/internal/app"
	"echo/apps/desktop/internal/config"

	"echo/apps/desktop/internal/keyboard"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func main() {
	settingsStore, err := config.NewDefaultStore()
	if err != nil {
		log.Fatalf("create settings store: %v", err)
	}
	if _, err := settingsStore.Load(); err != nil {
		log.Fatalf("load local settings: %v", err)
	}
	settingsService := app.NewSettingsService(settingsStore)
	app := application.New(application.Options{
		Name:        "echo",
		Description: "echo 桌面端骨架",
		Services: []application.Service{
			application.NewService(settingsService),
		},
		Assets: application.AssetOptions{
			Handler: assetHandler(),
		},
	})

	mainWindow := app.Window.NewWithOptions(mainWindowOptions())

	var allowQuit atomic.Bool
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if allowQuit.Load() {
			return
		}
		mainWindow.Hide()
		event.Cancel()
	})

	tray := app.SystemTray.New()
	tray.SetTooltip("echo")

	menu := app.NewMenu()
	menu.Add("显示主窗口").OnClick(func(_ *application.Context) {
		mainWindow.Show()
		mainWindow.Focus()
	})
	menu.AddSeparator()
	menu.Add("退出 echo").OnClick(func(_ *application.Context) {
		allowQuit.Store(true)
		app.Quit()
	})

	tray.AttachWindow(mainWindow).WindowOffset(5).SetMenu(menu)

	hookStatus := keyboard.HookStatus{Status: keyboard.HookStatusDisabled, Message: "native hook not started"}
	emitHookStatus := func() {
		app.Event.Emit(keyboard.HookStatusEventName, hookStatus)
	}
	app.Event.On(keyboard.HookStatusRequestEventName, func(_ *application.CustomEvent) {
		emitHookStatus()
	})

	keyboardHook := keyboard.NewHook(keyboard.DefaultTargetKey, func(event keyboard.Event) {
		app.Event.Emit(keyboard.PushToTalkEventName, event)
	})
	if err := keyboardHook.Start(); err != nil {
		log.Printf("keyboard hook disabled: %v", err)
		hookStatus = hookStatusFromError(err)
	} else {
		hookStatus = keyboard.HookStatus{Status: keyboard.HookStatusEnabled}
		defer keyboardHook.Stop()
	}
	emitHookStatus()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func mainWindowOptions() application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Title:            "echo",
		Width:            720,
		Height:           720,
		MinWidth:         600,
		MinHeight:        640,
		MaxWidth:         1000,
		MaxHeight:        900,
		BackgroundColour: application.NewRGB(243, 246, 248),
		URL:              "/",
	}
}

func hookStatusFromError(err error) keyboard.HookStatus {
	if errors.Is(err, keyboard.ErrUnsupported) {
		return keyboard.HookStatus{Status: keyboard.HookStatusUnsupported, Message: err.Error()}
	}
	return keyboard.HookStatus{Status: keyboard.HookStatusDisabled, Message: err.Error()}
}
