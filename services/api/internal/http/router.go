package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type routerConfig struct {
	roomCreator roomCreator
}

type RouterOption func(*routerConfig)

func WithRoomCreator(roomCreator roomCreator) RouterOption {
	return func(config *routerConfig) {
		config.roomCreator = roomCreator
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
	if config.roomCreator != nil {
		handlers := NewHandlers(config.roomCreator)
		v1 := router.Group("/v1")
		v1.POST("/rooms", handlers.CreateRoom)
	}
	return router
}
