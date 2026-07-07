//go:build production

package main

import (
	"embed"
	"net/http"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

func assetHandler() http.Handler {
	return application.AssetFileServerFS(assets)
}
