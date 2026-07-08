package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"echo/services/api/internal/domain"
	"echo/services/api/internal/room"
	"github.com/gin-gonic/gin"
)

const maxCreateRoomRequestBytes = 4096

type roomCreator interface {
	CreateContext(ctx context.Context, input room.CreateInput) (room.CreateResult, error)
}

type roomJoiner interface {
	JoinContext(ctx context.Context, input room.JoinInput) (room.JoinResult, error)
}

type roomLeaver interface {
	LeaveContext(ctx context.Context, input room.LeaveInput) (room.LeaveResult, error)
}

type Handlers struct {
	roomCreator roomCreator
	roomJoiner  roomJoiner
	roomLeaver  roomLeaver
}

func NewHandlers(roomCreator roomCreator, roomJoiner roomJoiner, roomLeaver roomLeaver) *Handlers {
	return &Handlers{roomCreator: roomCreator, roomJoiner: roomJoiner, roomLeaver: roomLeaver}
}

type createRoomRequest struct {
	AnonymousID string `json:"anonymous_id"`
	Nickname    string `json:"nickname"`
	AvatarID    string `json:"avatar_id"`
	RoomName    string `json:"room_name"`
}

type joinRoomRequest struct {
	InviteCode  string `json:"invite_code"`
	AnonymousID string `json:"anonymous_id"`
	Nickname    string `json:"nickname"`
	AvatarID    string `json:"avatar_id"`
}

type leaveRoomRequest struct {
	MemberID string `json:"member_id"`
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
		writeRoomError(c, err)
		return
	}

	c.JSON(http.StatusCreated, toCreateRoomResponse(result))
}

func (h *Handlers) JoinRoom(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRoomRequestBytes)

	var request joinRoomRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}

	result, err := h.roomJoiner.JoinContext(c.Request.Context(), room.JoinInput{
		InviteCode:  request.InviteCode,
		AnonymousID: request.AnonymousID,
		Nickname:    request.Nickname,
		AvatarID:    request.AvatarID,
	})
	if err != nil {
		writeRoomError(c, err)
		return
	}

	c.JSON(http.StatusOK, toJoinRoomResponse(result))
}

func (h *Handlers) LeaveRoom(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRoomRequestBytes)

	var request leaveRoomRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}

	_, err := h.roomLeaver.LeaveContext(c.Request.Context(), room.LeaveInput{
		RoomID:   c.Param("room_id"),
		MemberID: request.MemberID,
	})
	if err != nil {
		writeRoomError(c, err)
		return
	}

	c.Status(http.StatusNoContent)
}

func toCreateRoomResponse(result room.CreateResult) createRoomResponse {
	return toRoomMemberResponse(result.Room, result.Member)
}

func toJoinRoomResponse(result room.JoinResult) createRoomResponse {
	return toRoomMemberResponse(result.Room, result.Member)
}

func toRoomMemberResponse(roomValue domain.Room, memberValue domain.Member) createRoomResponse {
	return createRoomResponse{
		Room: roomResponse{
			ID:          roomValue.ID,
			Name:        roomValue.Name,
			InviteCode:  roomValue.InviteCode,
			State:       string(roomValue.State),
			CreatedAt:   roomValue.CreatedAt,
			LastEmptyAt: roomValue.LastEmptyAt,
			ExpiresAt:   roomValue.ExpiresAt,
		},
		Member: memberResponse{
			ID:              memberValue.ID,
			RoomID:          memberValue.RoomID,
			AnonymousID:     memberValue.AnonymousID,
			Nickname:        memberValue.Nickname,
			AvatarID:        memberValue.AvatarID,
			IsHost:          memberValue.IsHost,
			State:           string(memberValue.State),
			Muted:           memberValue.Muted,
			Speaking:        memberValue.Speaking,
			VoiceMode:       string(memberValue.VoiceMode),
			LiveKitIdentity: memberValue.LiveKitIdentity,
			JoinedAt:        memberValue.JoinedAt,
		},
	}
}

func writeRoomError(c *gin.Context, err error) {
	var validationErr *room.ValidationError
	if errors.As(err, &validationErr) {
		writeError(c, http.StatusBadRequest, validationErr.Code, validationErr.Message)
		return
	}
	if errors.Is(err, room.ErrInviteNotFound) {
		writeError(c, http.StatusNotFound, "invite_not_found", "邀请码无效，请检查后重试")
		return
	}
	if errors.Is(err, room.ErrRoomNotFound) {
		writeError(c, http.StatusNotFound, "room_not_found", "房间不存在或已失效")
		return
	}
	if errors.Is(err, room.ErrMemberNotFound) {
		writeError(c, http.StatusNotFound, "member_not_found", "成员不在房间中")
		return
	}
	if errors.Is(err, room.ErrRoomExpired) {
		writeError(c, http.StatusGone, "room_expired", "该房间已过期，请让朋友重新创建")
		return
	}
	if errors.Is(err, room.ErrRoomFull) {
		writeError(c, http.StatusConflict, "room_full", "房间人数已满，暂时无法加入")
		return
	}
	writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
}

func writeError(c *gin.Context, status int, code string, message string) {
	c.JSON(status, apiErrorResponse{Error: apiError{Code: code, Message: message}})
}
