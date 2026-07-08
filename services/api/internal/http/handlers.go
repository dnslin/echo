package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"echo/services/api/internal/room"
	"github.com/gin-gonic/gin"
)

const maxCreateRoomRequestBytes = 4096

type roomCreator interface {
	CreateContext(ctx context.Context, input room.CreateInput) (room.CreateResult, error)
}

type Handlers struct {
	roomCreator roomCreator
}

func NewHandlers(roomCreator roomCreator) *Handlers {
	return &Handlers{roomCreator: roomCreator}
}

type createRoomRequest struct {
	AnonymousID string `json:"anonymous_id"`
	Nickname    string `json:"nickname"`
	AvatarID    string `json:"avatar_id"`
	RoomName    string `json:"room_name"`
}

type createRoomResponse struct {
	Room   roomResponse   `json:"room"`
	Member memberResponse `json:"member"`
}

type roomResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	InviteCode  string     `json:"invite_code"`
	State       string     `json:"state"`
	CreatedAt   time.Time  `json:"created_at"`
	LastEmptyAt *time.Time `json:"last_empty_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type memberResponse struct {
	ID              string    `json:"id"`
	RoomID          string    `json:"room_id"`
	AnonymousID     string    `json:"anonymous_id"`
	Nickname        string    `json:"nickname"`
	AvatarID        string    `json:"avatar_id"`
	IsHost          bool      `json:"is_host"`
	State           string    `json:"state"`
	Muted           bool      `json:"muted"`
	Speaking        bool      `json:"speaking"`
	VoiceMode       string    `json:"voice_mode"`
	LiveKitIdentity string    `json:"livekit_identity"`
	JoinedAt        time.Time `json:"joined_at"`
}

type apiErrorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (h *Handlers) CreateRoom(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRoomRequestBytes)

	var request createRoomRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}

	result, err := h.roomCreator.CreateContext(c.Request.Context(), room.CreateInput{
		AnonymousID: request.AnonymousID,
		Nickname:    request.Nickname,
		AvatarID:    request.AvatarID,
		RoomName:    request.RoomName,
	})
	if err != nil {
		var validationErr *room.ValidationError
		if errors.As(err, &validationErr) {
			writeError(c, http.StatusBadRequest, validationErr.Code, validationErr.Message)
			return
		}
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
		return
	}

	c.JSON(http.StatusCreated, toCreateRoomResponse(result))
}

func toCreateRoomResponse(result room.CreateResult) createRoomResponse {
	return createRoomResponse{
		Room: roomResponse{
			ID:          result.Room.ID,
			Name:        result.Room.Name,
			InviteCode:  result.Room.InviteCode,
			State:       string(result.Room.State),
			CreatedAt:   result.Room.CreatedAt,
			LastEmptyAt: result.Room.LastEmptyAt,
			ExpiresAt:   result.Room.ExpiresAt,
		},
		Member: memberResponse{
			ID:              result.Member.ID,
			RoomID:          result.Member.RoomID,
			AnonymousID:     result.Member.AnonymousID,
			Nickname:        result.Member.Nickname,
			AvatarID:        result.Member.AvatarID,
			IsHost:          result.Member.IsHost,
			State:           string(result.Member.State),
			Muted:           result.Member.Muted,
			Speaking:        result.Member.Speaking,
			VoiceMode:       string(result.Member.VoiceMode),
			LiveKitIdentity: result.Member.LiveKitIdentity,
			JoinedAt:        result.Member.JoinedAt,
		},
	}
}

func writeError(c *gin.Context, status int, code string, message string) {
	c.JSON(status, apiErrorResponse{Error: apiError{Code: code, Message: message}})
}
