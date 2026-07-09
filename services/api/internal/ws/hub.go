package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"echo/services/api/internal/domain"
	"echo/services/api/internal/room"
	"echo/services/api/internal/session"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	DefaultHeartbeatInterval = 15 * time.Second
	DefaultHeartbeatTimeout  = 30 * time.Second
	DefaultReconnectWindow   = 30 * time.Second
	DefaultWriteTimeout      = 5 * time.Second
	DefaultConnectionQueue   = 16
)

type Authorizer interface {
	AuthorizeMemberContext(ctx context.Context, input room.AuthorizeMemberInput) (room.AuthorizeMemberResult, error)
}

type SnapshotStore interface {
	FindRoomByID(ctx context.Context, roomID string) (domain.Room, error)
	ListRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) ([]domain.Member, error)
}

type Config struct {
	Authorizer          Authorizer
	SnapshotStore       SnapshotStore
	RoomSessionSecret   string
	Now                 func() time.Time
	HeartbeatInterval   time.Duration
	HeartbeatTimeout    time.Duration
	ReconnectWindow     time.Duration
	WriteTimeout        time.Duration
	ConnectionQueueSize int
}

type Hub struct {
	authorizer          Authorizer
	snapshotStore       SnapshotStore
	roomSessionSecret   string
	now                 func() time.Time
	heartbeatInterval   time.Duration
	heartbeatTimeout    time.Duration
	reconnectWindow     time.Duration
	writeTimeout        time.Duration
	connectionQueueSize int

	mu          sync.Mutex
	rooms       map[string]*roomConnections
	pingCounter atomic.Int64
}

type roomConnections struct {
	seq         int64
	connections map[*connection]struct{}
	byMember    map[string]*connection
}

type connection struct {
	hub      *Hub
	ws       *websocket.Conn
	roomID   string
	memberID string

	ctx    context.Context
	cancel context.CancelFunc
	send   chan outboundMessage
	pong   chan string
	done   chan struct{}
	once   sync.Once
}

type outboundMessage struct {
	event       eventEnvelope
	closeAfter  bool
	closeCode   websocket.StatusCode
	closeReason string
}

type eventEnvelope struct {
	Type    string    `json:"type"`
	Seq     int64     `json:"seq"`
	SentAt  time.Time `json:"sent_at"`
	Payload any       `json:"payload"`
}

type roomProjection struct {
	RoomID      string     `json:"room_id"`
	Name        string     `json:"name"`
	InviteCode  string     `json:"invite_code"`
	State       string     `json:"state"`
	CreatedAt   time.Time  `json:"created_at"`
	LastEmptyAt *time.Time `json:"last_empty_at"`
	ExpiresAt   *time.Time `json:"expires_at"`
}

type snapshotMessagePayload struct {
	Room                roomProjection     `json:"room"`
	SelfMemberID        string             `json:"self_member_id"`
	Members             []memberProjection `json:"members"`
	LastSeq             int64              `json:"last_seq"`
	HeartbeatIntervalMS int                `json:"heartbeat_interval_ms"`
	HeartbeatTimeoutMS  int                `json:"heartbeat_timeout_ms"`
	ReconnectWindowMS   int                `json:"reconnect_window_ms"`
}

type memberProjection struct {
	MemberID       string     `json:"member_id"`
	Nickname       string     `json:"nickname"`
	AvatarID       string     `json:"avatar_id"`
	IsSelf         bool       `json:"is_self"`
	IsHost         bool       `json:"is_host"`
	State          string     `json:"state"`
	Muted          bool       `json:"muted"`
	Speaking       bool       `json:"speaking"`
	VoiceMode      string     `json:"voice_mode"`
	JoinedAt       time.Time  `json:"joined_at"`
	ReconnectUntil *time.Time `json:"reconnect_until"`
}

type joinedMessagePayload struct {
	Member memberProjection `json:"member"`
}

type leftMessagePayload struct {
	MemberID string    `json:"member_id"`
	LeftAt   time.Time `json:"left_at"`
}

type heartbeatMessagePayload struct {
	PingID     string    `json:"ping_id"`
	ServerTime time.Time `json:"server_time"`
}

type commandEnvelope struct {
	Type      string          `json:"type"`
	RequestID *string         `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

type pongCommandPayload struct {
	PingID string `json:"ping_id"`
}

type errorMessagePayload struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestID *string `json:"request_id"`
	Retryable bool    `json:"retryable"`
}

type apiErrorResponse struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type responseWriterUnwrapper interface {
	Unwrap() http.ResponseWriter
}

type responseWriterFlusher interface {
	WriteHeaderNow()
}

type upgradeWriter struct {
	original  http.ResponseWriter
	unwrapped http.ResponseWriter
}

func (w upgradeWriter) Header() http.Header {
	return w.original.Header()
}

func (w upgradeWriter) Write(data []byte) (int, error) {
	return w.original.Write(data)
}

func (w upgradeWriter) WriteHeader(statusCode int) {
	w.original.WriteHeader(statusCode)
	if flusher, ok := w.original.(responseWriterFlusher); ok {
		flusher.WriteHeaderNow()
	}
}

func (w upgradeWriter) Unwrap() http.ResponseWriter {
	return w.unwrapped
}

func upgradeResponseWriter(w http.ResponseWriter) http.ResponseWriter {
	if unwrapper, ok := w.(responseWriterUnwrapper); ok {
		return upgradeWriter{original: w, unwrapped: unwrapper.Unwrap()}
	}
	return w
}

func NewHub(config Config) *Hub {
	if config.HeartbeatInterval <= 0 {
		config.HeartbeatInterval = DefaultHeartbeatInterval
	}
	if config.HeartbeatTimeout <= 0 {
		config.HeartbeatTimeout = DefaultHeartbeatTimeout
	}
	if config.ReconnectWindow <= 0 {
		config.ReconnectWindow = DefaultReconnectWindow
	}
	if config.WriteTimeout <= 0 {
		config.WriteTimeout = DefaultWriteTimeout
	}
	if config.ConnectionQueueSize <= 0 {
		config.ConnectionQueueSize = DefaultConnectionQueue
	}
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Hub{
		authorizer:          config.Authorizer,
		snapshotStore:       config.SnapshotStore,
		roomSessionSecret:   config.RoomSessionSecret,
		now:                 now,
		heartbeatInterval:   config.HeartbeatInterval,
		heartbeatTimeout:    config.HeartbeatTimeout,
		reconnectWindow:     config.ReconnectWindow,
		writeTimeout:        config.WriteTimeout,
		connectionQueueSize: config.ConnectionQueueSize,
		rooms:               make(map[string]*roomConnections),
	}
}

func (h *Hub) ServeRoomHTTP(w http.ResponseWriter, r *http.Request, roomID string) {
	if err := h.validateConfig(); err != nil {
		writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务器错误")
		return
	}
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		writeHTTPError(w, http.StatusUnauthorized, "invalid_room_session", "连接凭证无效，请重新进入房间")
		return
	}
	claims, err := session.Verify(session.VerifyInput{Secret: h.roomSessionSecret, Token: token, Now: h.currentTime()})
	if err != nil {
		writeSessionHTTPError(w, err)
		return
	}
	pathRoomID := strings.TrimSpace(roomID)
	if claims.RoomID != pathRoomID {
		writeHTTPError(w, http.StatusForbidden, "room_session_mismatch", "连接凭证与房间不匹配")
		return
	}
	authorized, err := h.authorizer.AuthorizeMemberContext(r.Context(), room.AuthorizeMemberInput{RoomID: claims.RoomID, MemberID: claims.MemberID})
	if err != nil {
		writeAuthorizeHTTPError(w, err)
		return
	}

	wsConn, err := websocket.Accept(upgradeResponseWriter(w), r, &websocket.AcceptOptions{})
	if err != nil {
		return
	}
	client := h.newConnection(wsConn, authorized.Room.ID, authorized.Member.ID)
	snapshot, err := h.buildSnapshotEvent(client.ctx, client.roomID, client.memberID)
	if err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), h.writeTimeout)
		_ = wsjson.Write(ctx, wsConn, h.newRoomErrorEvent(client.roomID, nil, "internal_error", "服务器错误", true))
		cancel()
		_ = wsConn.Close(websocket.StatusInternalError, "snapshot failed")
		return
	}
	client.enqueue(outboundMessage{event: snapshot})
	h.register(client)
	go client.writeLoop()
	go client.readLoop()
	if h.heartbeatInterval > 0 && h.heartbeatTimeout > 0 {
		go client.heartbeatLoop()
	}
}

func (h *Hub) NotifyMemberJoined(_ context.Context, roomValue domain.Room, memberValue domain.Member) {
	if h == nil || strings.TrimSpace(roomValue.ID) == "" || strings.TrimSpace(memberValue.ID) == "" {
		return
	}
	event := h.newEvent(roomValue.ID, "member.joined", joinedMessagePayload{Member: projectMember(memberValue, "")})
	h.broadcast(roomValue.ID, outboundMessage{event: event})
}

func (h *Hub) NotifyMemberLeft(_ context.Context, roomValue domain.Room, memberValue domain.Member) {
	if h == nil || strings.TrimSpace(roomValue.ID) == "" || strings.TrimSpace(memberValue.ID) == "" {
		return
	}
	event := h.newEvent(roomValue.ID, "member.left", leftMessagePayload{MemberID: memberValue.ID, LeftAt: h.currentTime()})
	h.broadcastMemberLeft(roomValue.ID, memberValue.ID, event)
}

func (h *Hub) validateConfig() error {
	if h == nil || h.authorizer == nil || h.snapshotStore == nil || strings.TrimSpace(h.roomSessionSecret) == "" {
		return errors.New("websocket hub is not configured")
	}
	return nil
}

func (h *Hub) newConnection(wsConn *websocket.Conn, roomID string, memberID string) *connection {
	ctx, cancel := context.WithCancel(context.Background())
	return &connection{
		hub:      h,
		ws:       wsConn,
		roomID:   roomID,
		memberID: memberID,
		ctx:      ctx,
		cancel:   cancel,
		send:     make(chan outboundMessage, h.connectionQueueSize),
		pong:     make(chan string, 4),
		done:     make(chan struct{}),
	}
}

func (h *Hub) register(client *connection) {
	var old *connection
	h.mu.Lock()
	roomState := h.roomStateLocked(client.roomID)
	if existing := roomState.byMember[client.memberID]; existing != nil && existing != client {
		old = existing
		delete(roomState.connections, existing)
	}
	roomState.connections[client] = struct{}{}
	roomState.byMember[client.memberID] = client
	h.mu.Unlock()
	if old != nil {
		old.close(websocket.StatusNormalClosure, "connection replaced")
	}
}

func (h *Hub) unregister(client *connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	roomState := h.rooms[client.roomID]
	if roomState == nil {
		return
	}
	delete(roomState.connections, client)
	if roomState.byMember[client.memberID] == client {
		delete(roomState.byMember, client.memberID)
	}
}

func (h *Hub) roomStateLocked(roomID string) *roomConnections {
	roomState := h.rooms[roomID]
	if roomState == nil {
		roomState = &roomConnections{connections: make(map[*connection]struct{}), byMember: make(map[string]*connection)}
		h.rooms[roomID] = roomState
	}
	return roomState
}

func (h *Hub) nextSeq(roomID string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	roomState := h.roomStateLocked(roomID)
	roomState.seq++
	return roomState.seq
}

func (h *Hub) newEvent(roomID string, eventType string, payload any) eventEnvelope {
	return eventEnvelope{Type: eventType, Seq: h.nextSeq(roomID), SentAt: h.currentTime(), Payload: payload}
}

func (h *Hub) broadcast(roomID string, outbound outboundMessage) {
	clients := h.connectionsForRoom(roomID)
	for _, client := range clients {
		client.enqueue(outbound)
	}
}

func (h *Hub) broadcastMemberLeft(roomID string, memberID string, event eventEnvelope) {
	clients, leaving := h.connectionsForRoomAndMember(roomID, memberID)
	for _, client := range clients {
		outbound := outboundMessage{event: event}
		if client == leaving {
			outbound.closeAfter = true
			outbound.closeCode = websocket.StatusNormalClosure
			outbound.closeReason = "member left"
		}
		client.enqueue(outbound)
	}
}

func (h *Hub) connectionsForRoom(roomID string) []*connection {
	h.mu.Lock()
	defer h.mu.Unlock()
	roomState := h.rooms[roomID]
	if roomState == nil {
		return nil
	}
	clients := make([]*connection, 0, len(roomState.connections))
	for client := range roomState.connections {
		clients = append(clients, client)
	}
	return clients
}

func (h *Hub) connectionsForRoomAndMember(roomID string, memberID string) ([]*connection, *connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	roomState := h.rooms[roomID]
	if roomState == nil {
		return nil, nil
	}
	clients := make([]*connection, 0, len(roomState.connections))
	for client := range roomState.connections {
		clients = append(clients, client)
	}
	return clients, roomState.byMember[memberID]
}

func (h *Hub) currentTime() time.Time {
	if h == nil || h.now == nil {
		return time.Now().UTC()
	}
	now := h.now().UTC()
	if now.IsZero() {
		return time.Now().UTC()
	}
	return now
}

func (c *connection) sendSnapshot() {
	event, err := c.hub.buildSnapshotEvent(c.ctx, c.roomID, c.memberID)
	if err != nil {
		c.sendRoomError(nil, "internal_error", "服务器错误", true)
		c.enqueue(outboundMessage{event: eventEnvelope{}, closeAfter: true, closeCode: websocket.StatusInternalError, closeReason: "snapshot failed"})
		return
	}
	c.enqueue(outboundMessage{event: event})
}

func (h *Hub) buildSnapshotEvent(ctx context.Context, roomID string, selfMemberID string) (eventEnvelope, error) {
	roomValue, err := h.snapshotStore.FindRoomByID(ctx, roomID)
	if err != nil {
		return eventEnvelope{}, err
	}
	members, err := h.snapshotStore.ListRoomMembersByStates(ctx, roomID, activeMemberStates())
	if err != nil {
		return eventEnvelope{}, err
	}
	seq := h.nextSeq(roomID)
	return eventEnvelope{
		Type:   "room.snapshot",
		Seq:    seq,
		SentAt: h.currentTime(),
		Payload: snapshotMessagePayload{
			Room:                projectRoom(roomValue),
			SelfMemberID:        selfMemberID,
			Members:             projectMembers(members, selfMemberID),
			LastSeq:             seq,
			HeartbeatIntervalMS: durationMillis(h.heartbeatInterval),
			HeartbeatTimeoutMS:  durationMillis(h.heartbeatTimeout),
			ReconnectWindowMS:   durationMillis(h.reconnectWindow),
		},
	}, nil
}

func (c *connection) readLoop() {
	defer c.close(websocket.StatusNormalClosure, "read stopped")
	for {
		messageType, data, err := c.ws.Read(c.ctx)
		if err != nil {
			return
		}
		if messageType != websocket.MessageText {
			c.sendRoomError(nil, "invalid_message", "消息格式无效", false)
			continue
		}
		var command commandEnvelope
		if err := json.Unmarshal(data, &command); err != nil {
			c.sendRoomError(nil, "invalid_message", "消息格式无效", false)
			continue
		}
		command.Type = strings.TrimSpace(command.Type)
		switch command.Type {
		case "heartbeat.pong":
			c.handlePong(command.Payload, command.RequestID)
		case "room.resync_requested":
			c.sendSnapshot()
		case "":
			c.sendRoomError(command.RequestID, "invalid_message", "消息格式无效", false)
		default:
			c.sendRoomError(command.RequestID, "unknown_message_type", "消息类型无效", false)
		}
	}
}

func (c *connection) handlePong(raw json.RawMessage, requestID *string) {
	var payload pongCommandPayload
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || strings.TrimSpace(payload.PingID) == "" {
		c.sendRoomError(requestID, "invalid_message", "消息格式无效", false)
		return
	}
	select {
	case c.pong <- strings.TrimSpace(payload.PingID):
	default:
	}
}

func (c *connection) heartbeatLoop() {
	interval := c.hub.heartbeatInterval
	timeout := c.hub.heartbeatTimeout
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-timer.C:
		}
		pingID := fmt.Sprintf("ping-%d", c.hub.pingCounter.Add(1))
		event := c.hub.newEvent(c.roomID, "heartbeat.ping", heartbeatMessagePayload{PingID: pingID, ServerTime: c.hub.currentTime()})
		if !c.enqueue(outboundMessage{event: event}) {
			return
		}
		if !c.waitForPong(pingID, timeout) {
			c.close(websocket.StatusPolicyViolation, "heartbeat timeout")
			return
		}
		timer.Reset(interval)
	}
}

func (c *connection) waitForPong(pingID string, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	for {
		select {
		case <-c.done:
			return false
		case received := <-c.pong:
			if received == pingID {
				return true
			}
		case <-timer.C:
			return false
		}
	}
}

func (c *connection) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case outbound := <-c.send:
			if outbound.event.Type == "" {
				if outbound.closeAfter {
					c.close(outbound.closeCode, outbound.closeReason)
				}
				continue
			}
			ctx, cancel := context.WithTimeout(c.ctx, c.hub.writeTimeout)
			err := wsjson.Write(ctx, c.ws, outbound.event)
			cancel()
			if err != nil {
				c.close(websocket.StatusInternalError, "write failed")
				return
			}
			if outbound.closeAfter {
				c.close(outbound.closeCode, outbound.closeReason)
				return
			}
		}
	}
}

func (c *connection) enqueue(outbound outboundMessage) bool {
	select {
	case <-c.done:
		return false
	case c.send <- outbound:
		return true
	default:
		c.close(websocket.StatusPolicyViolation, "slow consumer")
		return false
	}
}

func (h *Hub) newRoomErrorEvent(roomID string, requestID *string, code string, message string, retryable bool) eventEnvelope {
	payload := errorMessagePayload{RequestID: requestID, Retryable: retryable}
	payload.Error.Code = code
	payload.Error.Message = message
	return h.newEvent(roomID, "room.error", payload)
}

func (c *connection) sendRoomError(requestID *string, code string, message string, retryable bool) {
	c.enqueue(outboundMessage{event: c.hub.newRoomErrorEvent(c.roomID, requestID, code, message, retryable)})
}

func (c *connection) close(code websocket.StatusCode, reason string) {
	c.once.Do(func() {
		c.cancel()
		_ = c.ws.Close(code, reason)
		c.hub.unregister(c)
		close(c.done)
	})
}

func projectRoom(roomValue domain.Room) roomProjection {
	return roomProjection{
		RoomID:      roomValue.ID,
		Name:        roomValue.Name,
		InviteCode:  roomValue.InviteCode,
		State:       string(roomValue.State),
		CreatedAt:   roomValue.CreatedAt,
		LastEmptyAt: roomValue.LastEmptyAt,
		ExpiresAt:   roomValue.ExpiresAt,
	}
}

func projectMembers(members []domain.Member, selfMemberID string) []memberProjection {
	projections := make([]memberProjection, 0, len(members))
	for _, member := range members {
		projections = append(projections, projectMember(member, selfMemberID))
	}
	return projections
}

func projectMember(member domain.Member, selfMemberID string) memberProjection {
	return memberProjection{
		MemberID:       member.ID,
		Nickname:       member.Nickname,
		AvatarID:       member.AvatarID,
		IsSelf:         selfMemberID != "" && member.ID == selfMemberID,
		IsHost:         member.IsHost,
		State:          string(member.State),
		Muted:          member.Muted,
		Speaking:       member.Speaking,
		VoiceMode:      string(member.VoiceMode),
		JoinedAt:       member.JoinedAt,
		ReconnectUntil: nil,
	}
}

func activeMemberStates() []domain.MemberState {
	return []domain.MemberState{domain.MemberStateOnline, domain.MemberStateReconnecting}
}

func durationMillis(duration time.Duration) int {
	return int(duration / time.Millisecond)
}

func writeSessionHTTPError(w http.ResponseWriter, err error) {
	if errors.Is(err, session.ErrExpiredToken) {
		writeHTTPError(w, http.StatusUnauthorized, "room_session_expired", "连接凭证已过期，请重新进入房间")
		return
	}
	writeHTTPError(w, http.StatusUnauthorized, "invalid_room_session", "连接凭证无效，请重新进入房间")
}

func writeAuthorizeHTTPError(w http.ResponseWriter, err error) {
	if errors.Is(err, room.ErrRoomNotFound) {
		writeHTTPError(w, http.StatusNotFound, "room_not_found", "房间不存在或已失效")
		return
	}
	if errors.Is(err, room.ErrRoomExpired) {
		writeHTTPError(w, http.StatusGone, "room_expired", "该房间已过期，请让朋友重新创建")
		return
	}
	if errors.Is(err, room.ErrMemberNotFound) || errors.Is(err, room.ErrMemberNotActive) {
		writeHTTPError(w, http.StatusForbidden, "member_not_active", "成员不在房间中")
		return
	}
	writeHTTPError(w, http.StatusInternalServerError, "internal_error", "服务器错误")
}

func writeHTTPError(w http.ResponseWriter, status int, code string, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(apiErrorResponse{Error: apiError{Code: code, Message: message}})
}
