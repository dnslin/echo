package main

import (
	"log/slog"
	"os"

	"echo/services/api/internal/config"
	httpapi "echo/services/api/internal/http"
	"echo/services/api/internal/invite"
	"echo/services/api/internal/room"
	"echo/services/api/internal/store"
	apiws "echo/services/api/internal/ws"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func main() {
	cfg := config.FromEnv()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	db, err := store.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		logger.Error("database open failed", "error", err.Error())
		os.Exit(1)
	}
	router := newRouter(cfg, db)

	logger.Info("api starting", "addr", cfg.HTTPAddr)
	if err := router.Run(cfg.HTTPAddr); err != nil {
		logger.Error("api stopped", "error", err.Error())
		os.Exit(1)
	}
}

func newRouter(cfg config.Config, db *gorm.DB) *gin.Engine {
	repository := store.NewRepository(db)
	roomService := room.NewService(repository, invite.NewGenerator())
	roomHub := apiws.NewHub(apiws.Config{
		Authorizer:        roomService,
		SnapshotStore:     repository,
		StateMutator:      roomService,
		RoomSessionSecret: cfg.RoomSessionSecret,
		OriginPatterns:    cfg.WebSocketOriginPatterns,
	})
	return httpapi.NewRouter(
		httpapi.WithRoomCreator(roomService),
		httpapi.WithRoomJoiner(roomService),
		httpapi.WithRoomLeaver(roomService),
		httpapi.WithRoomMemberAuthorizer(roomService),
		httpapi.WithRoomWebSocket(roomHub),
		httpapi.WithRoomEventNotifier(roomHub),
		httpapi.WithCredentialConfig(httpapi.CredentialConfig{
			LiveKitURL:          cfg.LiveKitURL,
			LiveKitAPIKey:       cfg.LiveKitAPIKey,
			LiveKitAPISecret:    cfg.LiveKitAPISecret,
			RoomSessionSecret:   cfg.RoomSessionSecret,
			RoomSessionTokenTTL: cfg.RoomSessionTokenTTL,
			LiveKitTokenTTL:     cfg.LiveKitTokenTTL,
		}),
	)
}
