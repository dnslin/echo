package main

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	app := application.New(application.Options{
		Name:        "echo",
		Description: "echo 桌面端骨架",
		Assets: application.AssetOptions{
			Handler: assetHandler(),
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "echo",
		Width:            1000,
		Height:           618,
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
