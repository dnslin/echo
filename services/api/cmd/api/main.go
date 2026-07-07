package main

import (
	"log/slog"
	"os"

	"echo/services/api/internal/config"
	httpapi "echo/services/api/internal/http"
)

func main() {
	cfg := config.Default()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	router := httpapi.NewRouter()

	logger.Info("api starting", "addr", cfg.HTTPAddr)
	if err := router.Run(cfg.HTTPAddr); err != nil {
		logger.Error("api stopped", "error", err.Error())
		os.Exit(1)
	}
}
