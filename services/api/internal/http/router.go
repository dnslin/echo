package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type routerConfig struct {
	roomCreator roomCreator
	roomJoiner  roomJoiner
	roomLeaver  roomLeaver
}

type RouterOption func(*routerConfig)

func WithRoomCreator(roomCreator roomCreator) RouterOption {
	return func(config *routerConfig) {
		config.roomCreator = roomCreator
	}
}

func WithRoomJoiner(roomJoiner roomJoiner) RouterOption {
	return func(config *routerConfig) {
		config.roomJoiner = roomJoiner
	}
}

func WithRoomLeaver(roomLeaver roomLeaver) RouterOption {
	return func(config *routerConfig) {
		config.roomLeaver = roomLeaver
	}
}

func NewRouter(options ...RouterOption) *gin.Engine {
	config := routerConfig{}
	for _, option := range options {
		option(&config)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	if config.roomCreator != nil || config.roomJoiner != nil || config.roomLeaver != nil {
		handlers := NewHandlers(config.roomCreator, config.roomJoiner, config.roomLeaver)
		v1 := router.Group("/v1")
		if config.roomCreator != nil {
			v1.POST("/rooms", handlers.CreateRoom)
		}
		if config.roomJoiner != nil {
			v1.POST("/rooms/join", handlers.JoinRoom)
		}
		if config.roomLeaver != nil {
			v1.POST("/rooms/:room_id/leave", handlers.LeaveRoom)
		}
	}
	return router
}
