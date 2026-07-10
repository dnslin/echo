package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"echo/services/api/internal/domain"
	apilivekit "echo/services/api/internal/livekit"
	"echo/services/api/internal/room"
	"echo/services/api/internal/session"
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

type roomMemberAuthorizer interface {
	AuthorizeMemberContext(ctx context.Context, input room.AuthorizeMemberInput) (room.AuthorizeMemberResult, error)
}

type roomEventNotifier interface {
	NotifyMemberJoined(ctx context.Context, roomValue domain.Room, memberValue domain.Member)
	NotifyMemberLeft(ctx context.Context, roomValue domain.Room, memberValue domain.Member)
}

type CredentialConfig struct {
	LiveKitURL          string
	LiveKitAPIKey       string
	LiveKitAPISecret    string
	RoomSessionSecret   string
	RoomSessionTokenTTL time.Duration
	LiveKitTokenTTL     time.Duration
	Now                 func() time.Time
}

type Handlers struct {
	roomCreator       roomCreator
	roomJoiner        roomJoiner
	roomLeaver        roomLeaver
	roomAuthorizer    roomMemberAuthorizer
	roomEventNotifier roomEventNotifier
	credentialConfig  CredentialConfig
}

func NewHandlers(roomCreator roomCreator, roomJoiner roomJoiner, roomLeaver roomLeaver, roomAuthorizer roomMemberAuthorizer, roomEventNotifier roomEventNotifier, credentialConfig CredentialConfig) *Handlers {
	return &Handlers{roomCreator: roomCreator, roomJoiner: roomJoiner, roomLeaver: roomLeaver, roomAuthorizer: roomAuthorizer, roomEventNotifier: roomEventNotifier, credentialConfig: credentialConfig}
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
	Room             roomResponse   `json:"room"`
	Member           memberResponse `json:"member"`
	RoomSessionToken string         `json:"room_session_token"`
	LiveKitURL       string         `json:"livekit_url"`
	LiveKitToken     string         `json:"livekit_token"`
}

type liveKitTokenResponse struct {
	LiveKitURL   string `json:"livekit_url"`
	LiveKitToken string `json:"livekit_token"`
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

	if err := validateCredentialConfig(h.credentialConfig); err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
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

	response, err := h.toRoomMemberCredentialResponse(result.Room, result.Member)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
		return
	}
	c.JSON(http.StatusCreated, response)
}

func (h *Handlers) JoinRoom(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRoomRequestBytes)

	var request joinRoomRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}

	if err := validateCredentialConfig(h.credentialConfig); err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
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

	response, err := h.toRoomMemberCredentialResponse(result.Room, result.Member)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
		return
	}
	if h.roomEventNotifier != nil {
		h.roomEventNotifier.NotifyMemberJoined(c.Request.Context(), result.Room, result.Member)
	}
	c.JSON(http.StatusOK, response)
}

func (h *Handlers) LeaveRoom(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxCreateRoomRequestBytes)

	var request leaveRoomRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		writeError(c, http.StatusBadRequest, "invalid_request", "请求格式无效")
		return
	}

	pathRoomID := strings.TrimSpace(c.Param("room_id"))
	memberID := strings.TrimSpace(request.MemberID)
	if pathRoomID == "" {
		writeError(c, http.StatusBadRequest, "invalid_room_id", "房间标识不能为空")
		return
	}
	if memberID == "" {
		writeError(c, http.StatusBadRequest, "invalid_member_id", "成员标识不能为空")
		return
	}
	if err := h.authorizeLeave(c, pathRoomID, memberID); err != nil {
		return
	}

	result, err := h.roomLeaver.LeaveContext(c.Request.Context(), room.LeaveInput{
		RoomID:   pathRoomID,
		MemberID: memberID,
	})
	if err != nil {
		writeRoomError(c, err)
		return
	}
	if result.Transitioned && h.roomEventNotifier != nil {
		h.roomEventNotifier.NotifyMemberLeft(c.Request.Context(), result.Room, result.Member)
	}

	c.Status(http.StatusNoContent)
}

func (h *Handlers) authorizeLeave(c *gin.Context, pathRoomID string, memberID string) error {
	token, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		writeError(c, http.StatusUnauthorized, "invalid_room_session", "连接凭证无效，请重新进入房间")
		return errors.New("missing room session")
	}
	if strings.TrimSpace(h.credentialConfig.RoomSessionSecret) == "" {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
		return errors.New("room session config is incomplete")
	}
	claims, err := session.Verify(session.VerifyInput{Secret: h.credentialConfig.RoomSessionSecret, Token: token, Now: h.now()})
	if err != nil {
		writeSessionError(c, err)
		return err
	}
	if claims.RoomID != pathRoomID || claims.MemberID != memberID {
		writeError(c, http.StatusForbidden, "room_session_mismatch", "连接凭证与房间不匹配")
		return errors.New("room session mismatch")
	}
	if h.roomAuthorizer == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
		return errors.New("room authorizer is not configured")
	}
	if _, err := h.roomAuthorizer.AuthorizeMemberContext(c.Request.Context(), room.AuthorizeMemberInput{RoomID: claims.RoomID, MemberID: claims.MemberID}); err != nil {
		writeCredentialRoomError(c, err)
		return err
	}
	return nil
}

func (h *Handlers) FreshLiveKitToken(c *gin.Context) {
	if err := validateCredentialConfig(h.credentialConfig); err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
		return
	}
	token, ok := bearerToken(c.GetHeader("Authorization"))
	if !ok {
		writeError(c, http.StatusUnauthorized, "invalid_room_session", "连接凭证无效，请重新进入房间")
		return
	}
	claims, err := session.Verify(session.VerifyInput{Secret: h.credentialConfig.RoomSessionSecret, Token: token, Now: h.now()})
	if err != nil {
		writeSessionError(c, err)
		return
	}
	pathRoomID := strings.TrimSpace(c.Param("room_id"))
	if claims.RoomID != pathRoomID {
		writeError(c, http.StatusForbidden, "room_session_mismatch", "连接凭证与房间不匹配")
		return
	}
	if h.roomAuthorizer == nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
		return
	}
	result, err := h.roomAuthorizer.AuthorizeMemberContext(c.Request.Context(), room.AuthorizeMemberInput{RoomID: claims.RoomID, MemberID: claims.MemberID})
	if err != nil {
		writeCredentialRoomError(c, err)
		return
	}
	liveKitToken, err := h.liveKitToken(result.Room, result.Member)
	if err != nil {
		writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
		return
	}
	c.JSON(http.StatusOK, liveKitTokenResponse{LiveKitURL: strings.TrimSpace(h.credentialConfig.LiveKitURL), LiveKitToken: liveKitToken})
}

func (h *Handlers) toRoomMemberCredentialResponse(roomValue domain.Room, memberValue domain.Member) (createRoomResponse, error) {
	if err := validateCredentialConfig(h.credentialConfig); err != nil {
		return createRoomResponse{}, err
	}
	roomSessionToken, _, err := session.Sign(session.SignInput{
		Secret:   h.credentialConfig.RoomSessionSecret,
		RoomID:   roomValue.ID,
		MemberID: memberValue.ID,
		Now:      h.now(),
		TTL:      h.credentialConfig.RoomSessionTokenTTL,
	})
	if err != nil {
		return createRoomResponse{}, err
	}
	liveKitToken, err := h.liveKitToken(roomValue, memberValue)
	if err != nil {
		return createRoomResponse{}, err
	}
	response := toRoomMemberResponse(roomValue, memberValue)
	response.RoomSessionToken = roomSessionToken
	response.LiveKitURL = strings.TrimSpace(h.credentialConfig.LiveKitURL)
	response.LiveKitToken = liveKitToken
	return response, nil
}

func (h *Handlers) liveKitToken(roomValue domain.Room, memberValue domain.Member) (string, error) {
	return apilivekit.JoinToken(apilivekit.JoinTokenInput{
		APIKey:    h.credentialConfig.LiveKitAPIKey,
		APISecret: h.credentialConfig.LiveKitAPISecret,
		RoomName:  roomValue.LiveKitRoomName,
		Identity:  memberValue.LiveKitIdentity,
		Name:      memberValue.Nickname,
		ValidFor:  h.credentialConfig.LiveKitTokenTTL,
	})
}

func (h *Handlers) now() time.Time {
	if h.credentialConfig.Now != nil {
		return h.credentialConfig.Now().UTC()
	}
	return time.Now().UTC()
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

func validateCredentialConfig(config CredentialConfig) error {
	if strings.TrimSpace(config.LiveKitURL) == "" || strings.TrimSpace(config.LiveKitAPIKey) == "" || strings.TrimSpace(config.LiveKitAPISecret) == "" || strings.TrimSpace(config.RoomSessionSecret) == "" {
		return errors.New("credential config is incomplete")
	}
	if config.RoomSessionTokenTTL <= 0 || config.LiveKitTokenTTL <= 0 {
		return errors.New("credential ttl config is invalid")
	}
	return nil
}

func bearerToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	if len(header) < len("Bearer ") || !strings.EqualFold(header[:len("Bearer")], "Bearer") || header[len("Bearer")] != ' ' {
		return "", false
	}
	token := strings.TrimSpace(header[len("Bearer "):])
	return token, token != ""
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

func writeCredentialRoomError(c *gin.Context, err error) {
	var validationErr *room.ValidationError
	if errors.As(err, &validationErr) {
		writeError(c, http.StatusBadRequest, validationErr.Code, validationErr.Message)
		return
	}
	if errors.Is(err, room.ErrRoomNotFound) {
		writeError(c, http.StatusNotFound, "room_not_found", "房间不存在或已失效")
		return
	}
	if errors.Is(err, room.ErrRoomExpired) {
		writeError(c, http.StatusGone, "room_expired", "该房间已过期，请让朋友重新创建")
		return
	}
	if errors.Is(err, room.ErrMemberNotFound) || errors.Is(err, room.ErrMemberNotActive) {
		writeError(c, http.StatusForbidden, "member_not_active", "成员不在房间中")
		return
	}
	writeError(c, http.StatusInternalServerError, "internal_error", "服务器错误")
}

func writeSessionError(c *gin.Context, err error) {
	if errors.Is(err, session.ErrExpiredToken) {
		writeError(c, http.StatusUnauthorized, "room_session_expired", "连接凭证已过期，请重新进入房间")
		return
	}
	writeError(c, http.StatusUnauthorized, "invalid_room_session", "连接凭证无效，请重新进入房间")
}

func writeError(c *gin.Context, status int, code string, message string) {
	c.JSON(status, apiErrorResponse{Error: apiError{Code: code, Message: message}})
}
