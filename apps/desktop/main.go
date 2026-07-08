package main

import (
	"log"
	"sync/atomic"

	"echo/apps/desktop/internal/keyboard"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func main() {
	app := application.New(application.Options{
		Name:        "echo",
		Description: "echo 桌面端骨架",
		Assets: application.AssetOptions{
			Handler: assetHandler(),
		},
	})

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "echo",
		Width:            1000,
		Height:           618,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

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

	keyboardHook := keyboard.NewHook(keyboard.DefaultTargetKey, func(event keyboard.Event) {
		app.Event.Emit(keyboard.PushToTalkEventName, event)
	})
	if err := keyboardHook.Start(); err != nil {
		log.Printf("keyboard hook disabled: %v", err)
	} else {
		defer keyboardHook.Stop()
	}

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
