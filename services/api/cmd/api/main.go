package main

import (
	"log/slog"
	"os"

	"echo/services/api/internal/config"
	httpapi "echo/services/api/internal/http"
	"echo/services/api/internal/invite"
	"echo/services/api/internal/room"
	"echo/services/api/internal/store"
)

func main() {
	cfg := config.Default()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db, err := store.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		logger.Error("database open failed", "error", err.Error())
		os.Exit(1)
	}
	roomService := room.NewService(store.NewRepository(db), invite.NewGenerator())
	router := httpapi.NewRouter(httpapi.WithRoomCreator(roomService), httpapi.WithRoomJoiner(roomService), httpapi.WithRoomLeaver(roomService))

	logger.Info("api starting", "addr", cfg.HTTPAddr)
	if err := router.Run(cfg.HTTPAddr); err != nil {
		logger.Error("api stopped", "error", err.Error())
		os.Exit(1)
	}
}
