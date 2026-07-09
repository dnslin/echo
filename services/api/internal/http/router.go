package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type roomWebSocket interface {
	ServeRoomHTTP(w http.ResponseWriter, r *http.Request, roomID string)
}

type routerConfig struct {
	roomCreator       roomCreator
	roomJoiner        roomJoiner
	roomLeaver        roomLeaver
	roomAuthorizer    roomMemberAuthorizer
	roomEventNotifier roomEventNotifier
	roomWebSocket     roomWebSocket
	credentialConfig  CredentialConfig
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

func WithRoomMemberAuthorizer(roomAuthorizer roomMemberAuthorizer) RouterOption {
	return func(config *routerConfig) {
		config.roomAuthorizer = roomAuthorizer
	}
}

func WithRoomEventNotifier(roomEventNotifier roomEventNotifier) RouterOption {
	return func(config *routerConfig) {
		config.roomEventNotifier = roomEventNotifier
	}
}

func WithRoomWebSocket(roomWebSocket roomWebSocket) RouterOption {
	return func(config *routerConfig) {
		config.roomWebSocket = roomWebSocket
	}
}

func WithCredentialConfig(credentialConfig CredentialConfig) RouterOption {
	return func(config *routerConfig) {
		config.credentialConfig = credentialConfig
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
	if config.roomCreator != nil || config.roomJoiner != nil || config.roomLeaver != nil || config.roomAuthorizer != nil || config.roomWebSocket != nil {
		handlers := NewHandlers(config.roomCreator, config.roomJoiner, config.roomLeaver, config.roomAuthorizer, config.roomEventNotifier, config.credentialConfig)
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
		if config.roomAuthorizer != nil {
			v1.POST("/rooms/:room_id/livekit-token", handlers.FreshLiveKitToken)
		}
		if config.roomWebSocket != nil {
			v1.GET("/rooms/:room_id/ws", func(c *gin.Context) {
				originalRequest := c.Request
				c.Request = redactedTokenRequest(originalRequest)
				config.roomWebSocket.ServeRoomHTTP(c.Writer, originalRequest, c.Param("room_id"))
				c.Request = originalRequest
			})
		}
	}
	return router
}

func redactedTokenRequest(request *http.Request) *http.Request {
	if request == nil || request.URL == nil {
		return request
	}
	redacted := request.Clone(request.Context())
	urlCopy := *request.URL
	query := urlCopy.Query()
	if query.Has("token") {
		query.Set("token", "redacted")
		urlCopy.RawQuery = query.Encode()
	}
	redacted.URL = &urlCopy
	return redacted
}
