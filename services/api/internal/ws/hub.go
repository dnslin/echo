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
	DefaultHeartbeatInterval   = 15 * time.Second
	DefaultHeartbeatTimeout    = 30 * time.Second
	DefaultReconnectWindow     = 30 * time.Second
	DefaultWriteTimeout        = 5 * time.Second
	DefaultConnectionQueue     = 16
	DefaultResyncMinInterval   = time.Second
	DefaultSpeakingMinInterval = 100 * time.Millisecond
)

type Authorizer interface {
	AuthorizeMemberContext(ctx context.Context, input room.AuthorizeMemberInput) (room.AuthorizeMemberResult, error)
}

type SnapshotStore interface {
	FindRoomByID(ctx context.Context, roomID string) (domain.Room, error)
	ListRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) ([]domain.Member, error)
}

type StateMutator interface {
	UpdateMemberMuteContext(ctx context.Context, input room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error)
	UpdateMemberSpeakingContext(ctx context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error)
	DisconnectMemberContext(ctx context.Context, input room.DisconnectMemberInput) (room.LeaveResult, error)
}

type Config struct {
	Authorizer          Authorizer
	SnapshotStore       SnapshotStore
	StateMutator        StateMutator
	RoomSessionSecret   string
	Now                 func() time.Time
	HeartbeatInterval   time.Duration
	HeartbeatTimeout    time.Duration
	ReconnectWindow     time.Duration
	WriteTimeout        time.Duration
	ConnectionQueueSize int
	OriginPatterns      []string
	ResyncMinInterval   time.Duration
	SpeakingMinInterval time.Duration
}

type Hub struct {
	authorizer          Authorizer
	snapshotStore       SnapshotStore
	stateMutator        StateMutator
	roomSessionSecret   string
	now                 func() time.Time
	heartbeatInterval   time.Duration
	heartbeatTimeout    time.Duration
	reconnectWindow     time.Duration
	writeTimeout        time.Duration
	connectionQueueSize int
	originPatterns      []string
	resyncMinInterval   time.Duration
	speakingMinInterval time.Duration

	mu               sync.Mutex
	rooms            map[string]*roomConnections
	pingCounter      atomic.Int64
	reconnectCounter atomic.Int64
}

type reconnectingMember struct {
	deadline   time.Time
	generation int64
	timer      *time.Timer
	retries    int
}

type roomConnections struct {
	seq                  int64
	connections          map[*connection]struct{}
	byMember             map[string]*connection
	reconnecting         map[string]reconnectingMember
	lastSpeakingAccepted map[string]time.Time
}

type connection struct {
	hub      *Hub
	ws       *websocket.Conn
	roomID   string
	memberID string

	ctx          context.Context
	cancel       context.CancelFunc
	send         chan outboundMessage
	pong         chan string
	done         chan struct{}
	once         sync.Once
	lastResyncAt time.Time
}

type outboundMessage struct {
	event       eventEnvelope
	closeAfter  bool
	closeCode   websocket.StatusCode
	closeReason string
	closeMode   closeMode
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

type mutedChangedMessagePayload struct {
	MemberID  string    `json:"member_id"`
	Muted     bool      `json:"muted"`
	ChangedAt time.Time `json:"changed_at"`
}

type speakingChangedMessagePayload struct {
	MemberID  string    `json:"member_id"`
	Speaking  bool      `json:"speaking"`
	ChangedAt time.Time `json:"changed_at"`
}

type reconnectingMessagePayload struct {
	MemberID          string    `json:"member_id"`
	ReconnectUntil    time.Time `json:"reconnect_until"`
	ReconnectWindowMS int       `json:"reconnect_window_ms"`
}

type restoredMessagePayload struct {
	Member     memberProjection `json:"member"`
	RestoredAt time.Time        `json:"restored_at"`
}

type disconnectedMessagePayload struct {
	MemberID       string    `json:"member_id"`
	DisconnectedAt time.Time `json:"disconnected_at"`
	Reason         string    `json:"reason"`
}

type commandEnvelope struct {
	Type      string          `json:"type"`
	RequestID *string         `json:"request_id"`
	Payload   json.RawMessage `json:"payload"`
}

type pongCommandPayload struct {
	PingID string `json:"ping_id"`
}

type muteCommandPayload struct {
	Muted *bool `json:"muted"`
}

type speakingCommandPayload struct {
	Speaking *bool `json:"speaking"`
}

type closeMode int

const (
	closeModeReconnect closeMode = iota
	closeModeNoReconnect
)

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
	if config.ResyncMinInterval <= 0 {
		config.ResyncMinInterval = DefaultResyncMinInterval
	}
	if config.SpeakingMinInterval <= 0 {
		config.SpeakingMinInterval = DefaultSpeakingMinInterval
	}
	now := config.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Hub{
		authorizer:          config.Authorizer,
		snapshotStore:       config.SnapshotStore,
		stateMutator:        config.StateMutator,
		roomSessionSecret:   config.RoomSessionSecret,
		now:                 now,
		heartbeatInterval:   config.HeartbeatInterval,
		heartbeatTimeout:    config.HeartbeatTimeout,
		reconnectWindow:     config.ReconnectWindow,
		writeTimeout:        config.WriteTimeout,
		connectionQueueSize: config.ConnectionQueueSize,
		originPatterns:      append([]string(nil), config.OriginPatterns...),
		resyncMinInterval:   config.ResyncMinInterval,
		speakingMinInterval: config.SpeakingMinInterval,
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

	wsConn, err := websocket.Accept(upgradeResponseWriter(w), r, &websocket.AcceptOptions{OriginPatterns: h.originPatterns})
	if err != nil {
		return
	}
	client := h.newConnection(wsConn, authorized.Room.ID, authorized.Member.ID)
	if err := h.registerWithInitialSnapshot(client, authorized.Member); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), h.writeTimeout)
		_ = wsjson.Write(ctx, wsConn, h.newRoomErrorEvent(client.roomID, nil, "internal_error", "服务器错误", true))
		cancel()
		_ = wsConn.Close(websocket.StatusInternalError, "snapshot failed")
		return
	}
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
	var slowClients []*connection
	h.mu.Lock()
	roomState := h.rooms[roomValue.ID]
	if roomState == nil || len(roomState.connections) == 0 {
		h.mu.Unlock()
		return
	}
	roomState.seq++
	event := eventEnvelope{Type: "member.joined", Seq: roomState.seq, SentAt: h.currentTime(), Payload: joinedMessagePayload{Member: projectMember(memberValue, "", nil)}}
	for client := range roomState.connections {
		if !client.enqueueLocked(outboundMessage{event: event}) {
			slowClients = append(slowClients, client)
		}
	}
	h.mu.Unlock()
	closeSlowClients(slowClients)
}

func (h *Hub) NotifyMemberLeft(_ context.Context, roomValue domain.Room, memberValue domain.Member) {
	if h == nil || strings.TrimSpace(roomValue.ID) == "" || strings.TrimSpace(memberValue.ID) == "" {
		return
	}
	var slowClients []*connection
	h.mu.Lock()
	roomState := h.rooms[roomValue.ID]
	if roomState == nil {
		h.mu.Unlock()
		return
	}
	if reconnecting, ok := roomState.reconnecting[memberValue.ID]; ok {
		if reconnecting.timer != nil {
			reconnecting.timer.Stop()
		}
		delete(roomState.reconnecting, memberValue.ID)
	}
	if len(roomState.connections) == 0 {
		h.pruneRoomStateLocked(roomValue.ID, roomState)
		h.mu.Unlock()
		return
	}
	roomState.seq++
	event := eventEnvelope{Type: "member.left", Seq: roomState.seq, SentAt: h.currentTime(), Payload: leftMessagePayload{MemberID: memberValue.ID, LeftAt: h.currentTime()}}
	leaving := roomState.byMember[memberValue.ID]
	for client := range roomState.connections {
		outbound := outboundMessage{event: event}
		if client == leaving {
			outbound.closeAfter = true
			outbound.closeCode = websocket.StatusNormalClosure
			outbound.closeReason = "member left"
			outbound.closeMode = closeModeNoReconnect
		}
		if !client.enqueueLocked(outbound) {
			slowClients = append(slowClients, client)
		}
	}
	h.mu.Unlock()
	closeSlowClients(slowClients)
}

func (h *Hub) validateConfig() error {
	if h == nil || h.authorizer == nil || h.snapshotStore == nil || h.stateMutator == nil || strings.TrimSpace(h.roomSessionSecret) == "" {
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

func (h *Hub) registerWithInitialSnapshot(client *connection, authorizedMember domain.Member) error {
	var old *connection
	var slowClients []*connection
	h.mu.Lock()
	roomState := h.roomStateLocked(client.roomID)
	sharedSeq := roomState.seq
	_, restoring := roomState.reconnecting[client.memberID]
	snapshot, err := h.buildSnapshotEventAtSeqLocked(client.ctx, client.roomID, client.memberID, sharedSeq, client.memberID)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	if !client.enqueueLocked(outboundMessage{event: snapshot}) {
		h.mu.Unlock()
		client.close(websocket.StatusPolicyViolation, "slow consumer")
		return errors.New("initial snapshot queue is full")
	}
	if existing := roomState.byMember[client.memberID]; existing != nil && existing != client {
		old = existing
		delete(roomState.connections, existing)
	}
	roomState.connections[client] = struct{}{}
	roomState.byMember[client.memberID] = client
	if restoring {
		if reconnecting := roomState.reconnecting[client.memberID]; reconnecting.timer != nil {
			reconnecting.timer.Stop()
		}
		roomState.seq++
		restoredEvent := eventEnvelope{
			Type:   "member.restored",
			Seq:    roomState.seq,
			SentAt: h.currentTime(),
			Payload: restoredMessagePayload{
				Member:     projectMember(authorizedMember, "", nil),
				RestoredAt: h.currentTime(),
			},
		}
		for candidate := range roomState.connections {
			if !candidate.enqueueLocked(outboundMessage{event: restoredEvent}) {
				slowClients = append(slowClients, candidate)
			}
		}
		delete(roomState.reconnecting, client.memberID)
	}
	h.mu.Unlock()
	if old != nil {
		old.closeWithMode(closeModeNoReconnect, websocket.StatusNormalClosure, "connection replaced")
	}
	closeSlowClients(slowClients)
	return nil
}

func (h *Hub) unregister(client *connection) {
	h.mu.Lock()
	defer h.mu.Unlock()
	roomState := h.rooms[client.roomID]
	if roomState == nil {
		return
	}
	h.removeConnectionLocked(roomState, client)
	h.pruneRoomStateLocked(client.roomID, roomState)
}

func (h *Hub) removeConnectionLocked(roomState *roomConnections, client *connection) {
	delete(roomState.connections, client)
	if roomState.byMember[client.memberID] == client {
		delete(roomState.byMember, client.memberID)
	}
}

func (h *Hub) pruneRoomStateLocked(roomID string, roomState *roomConnections) {
	if len(roomState.connections) == 0 && len(roomState.reconnecting) == 0 {
		delete(h.rooms, roomID)
	}
}

func (h *Hub) roomStateLocked(roomID string) *roomConnections {
	roomState := h.rooms[roomID]
	if roomState == nil {
		roomState = &roomConnections{
			connections:          make(map[*connection]struct{}),
			byMember:             make(map[string]*connection),
			reconnecting:         make(map[string]reconnectingMember),
			lastSpeakingAccepted: make(map[string]time.Time),
		}
		h.rooms[roomID] = roomState
	}
	return roomState
}

func (h *Hub) currentSeq(roomID string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.currentSeqLocked(roomID)
}

func (h *Hub) currentSeqLocked(roomID string) int64 {
	roomState := h.rooms[roomID]
	if roomState == nil {
		return 0
	}
	return roomState.seq
}

func (h *Hub) newPrivateEvent(roomID string, eventType string, payload any) eventEnvelope {
	return eventEnvelope{Type: eventType, Seq: h.currentSeq(roomID), SentAt: h.currentTime(), Payload: payload}
}

func (h *Hub) newSharedEventForRoom(roomID string, eventType string, payload any) (eventEnvelope, []*connection, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	roomState := h.rooms[roomID]
	if roomState == nil || len(roomState.connections) == 0 {
		return eventEnvelope{}, nil, false
	}
	roomState.seq++
	event := eventEnvelope{Type: eventType, Seq: roomState.seq, SentAt: h.currentTime(), Payload: payload}
	clients := make([]*connection, 0, len(roomState.connections))
	for client := range roomState.connections {
		clients = append(clients, client)
	}
	return event, clients, true
}

func (h *Hub) newSharedMemberLeftEvent(roomID string, memberID string, payload leftMessagePayload) (eventEnvelope, []*connection, *connection, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	roomState := h.rooms[roomID]
	if roomState == nil || len(roomState.connections) == 0 {
		return eventEnvelope{}, nil, nil, false
	}
	roomState.seq++
	event := eventEnvelope{Type: "member.left", Seq: roomState.seq, SentAt: h.currentTime(), Payload: payload}
	clients := make([]*connection, 0, len(roomState.connections))
	for client := range roomState.connections {
		clients = append(clients, client)
	}
	return event, clients, roomState.byMember[memberID], true
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
	var slowClient bool
	c.hub.mu.Lock()
	event, err := c.hub.buildSnapshotEventAtSeqLocked(c.ctx, c.roomID, c.memberID, c.hub.currentSeqLocked(c.roomID), "")
	if err == nil {
		slowClient = !c.enqueueLocked(outboundMessage{event: event})
	}
	c.hub.mu.Unlock()
	if err != nil {
		c.sendRoomError(nil, "internal_error", "服务器错误", true)
		c.enqueue(outboundMessage{event: eventEnvelope{}, closeAfter: true, closeCode: websocket.StatusInternalError, closeReason: "snapshot failed"})
		return
	}
	if slowClient {
		c.close(websocket.StatusPolicyViolation, "slow consumer")
	}
}

func (h *Hub) buildSnapshotEvent(ctx context.Context, roomID string, selfMemberID string) (eventEnvelope, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.buildSnapshotEventAtSeqLocked(ctx, roomID, selfMemberID, h.currentSeqLocked(roomID), "")
}

func (h *Hub) buildSnapshotEventAtSeqLocked(ctx context.Context, roomID string, selfMemberID string, seq int64, ignoreReconnectMemberID string) (eventEnvelope, error) {
	roomValue, err := h.snapshotStore.FindRoomByID(ctx, roomID)
	if err != nil {
		return eventEnvelope{}, err
	}
	members, err := h.snapshotStore.ListRoomMembersByStates(ctx, roomID, activeMemberStates())
	if err != nil {
		return eventEnvelope{}, err
	}
	reconnecting := map[string]reconnectingMember(nil)
	if roomState := h.rooms[roomID]; roomState != nil {
		reconnecting = roomState.reconnecting
	}
	return eventEnvelope{
		Type:   "room.snapshot",
		Seq:    seq,
		SentAt: h.currentTime(),
		Payload: snapshotMessagePayload{
			Room:                projectRoom(roomValue),
			SelfMemberID:        selfMemberID,
			Members:             projectMembers(members, selfMemberID, reconnecting, ignoreReconnectMemberID),
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
			c.handleResync(command.RequestID)
		case "member.mute_changed":
			c.handleMuteChanged(command.Payload, command.RequestID)
		case "member.speaking_changed":
			c.handleSpeakingChanged(command.Payload, command.RequestID)
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

func (c *connection) handleResync(requestID *string) {
	now := c.hub.currentTime()
	if c.hub.resyncMinInterval > 0 && !c.lastResyncAt.IsZero() && now.Sub(c.lastResyncAt) < c.hub.resyncMinInterval {
		c.sendRoomError(requestID, "rate_limited", "操作过于频繁，请稍后重试", true)
		return
	}
	c.lastResyncAt = now
	c.sendSnapshot()
}

func (c *connection) handleMuteChanged(raw json.RawMessage, requestID *string) {
	var payload muteCommandPayload
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || payload.Muted == nil {
		c.sendRoomError(requestID, "invalid_message", "消息格式无效", false)
		return
	}
	result, err := c.hub.stateMutator.UpdateMemberMuteContext(c.ctx, room.UpdateMemberMuteInput{
		RoomID:   c.roomID,
		MemberID: c.memberID,
		Muted:    *payload.Muted,
	})
	if err != nil {
		c.sendStateMutationError(requestID, err)
		return
	}
	if !result.MutedChanged && !result.SpeakingChanged {
		return
	}

	now := c.hub.currentTime()
	var slowClients []*connection
	c.hub.mu.Lock()
	roomState := c.hub.rooms[c.roomID]
	if roomState != nil && len(roomState.connections) > 0 {
		if result.SpeakingChanged {
			roomState.lastSpeakingAccepted[c.memberID] = now
			roomState.seq++
			event := eventEnvelope{
				Type:   "member.speaking_changed",
				Seq:    roomState.seq,
				SentAt: now,
				Payload: speakingChangedMessagePayload{
					MemberID:  result.Member.ID,
					Speaking:  result.Member.Speaking,
					ChangedAt: now,
				},
			}
			for client := range roomState.connections {
				if !client.enqueueLocked(outboundMessage{event: event}) {
					slowClients = append(slowClients, client)
				}
			}
		}
		if result.MutedChanged {
			roomState.seq++
			event := eventEnvelope{
				Type:   "member.muted_changed",
				Seq:    roomState.seq,
				SentAt: now,
				Payload: mutedChangedMessagePayload{
					MemberID:  result.Member.ID,
					Muted:     result.Member.Muted,
					ChangedAt: now,
				},
			}
			for client := range roomState.connections {
				if !client.enqueueLocked(outboundMessage{event: event}) {
					slowClients = append(slowClients, client)
				}
			}
		}
	}
	c.hub.mu.Unlock()
	closeSlowClients(slowClients)
}

func (c *connection) handleSpeakingChanged(raw json.RawMessage, requestID *string) {
	var payload speakingCommandPayload
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || payload.Speaking == nil {
		c.sendRoomError(requestID, "invalid_message", "消息格式无效", false)
		return
	}
	now := c.hub.currentTime()
	if *payload.Speaking && c.hub.shouldThrottleSpeaking(c.roomID, c.memberID, now) {
		return
	}
	result, err := c.hub.stateMutator.UpdateMemberSpeakingContext(c.ctx, room.UpdateMemberSpeakingInput{
		RoomID:   c.roomID,
		MemberID: c.memberID,
		Speaking: *payload.Speaking,
	})
	if err != nil {
		c.sendStateMutationError(requestID, err)
		return
	}
	if !result.Changed {
		return
	}

	var slowClients []*connection
	c.hub.mu.Lock()
	roomState := c.hub.rooms[c.roomID]
	if roomState != nil && len(roomState.connections) > 0 {
		roomState.lastSpeakingAccepted[c.memberID] = now
		roomState.seq++
		event := eventEnvelope{
			Type:   "member.speaking_changed",
			Seq:    roomState.seq,
			SentAt: now,
			Payload: speakingChangedMessagePayload{
				MemberID:  result.Member.ID,
				Speaking:  result.Member.Speaking,
				ChangedAt: now,
			},
		}
		for client := range roomState.connections {
			if !client.enqueueLocked(outboundMessage{event: event}) {
				slowClients = append(slowClients, client)
			}
		}
	}
	c.hub.mu.Unlock()
	closeSlowClients(slowClients)
}

func (h *Hub) shouldThrottleSpeaking(roomID string, memberID string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	roomState := h.rooms[roomID]
	if roomState == nil {
		return false
	}
	lastAccepted := roomState.lastSpeakingAccepted[memberID]
	return h.speakingMinInterval > 0 && !lastAccepted.IsZero() && now.Sub(lastAccepted) < h.speakingMinInterval
}

func (c *connection) sendStateMutationError(requestID *string, err error) {
	var validationErr *room.ValidationError
	if errors.As(err, &validationErr) {
		c.sendRoomError(requestID, "invalid_message", "消息格式无效", false)
		return
	}
	if errors.Is(err, room.ErrRoomExpired) || errors.Is(err, room.ErrRoomNotFound) {
		c.sendRoomError(requestID, "room_expired", "该房间已过期，请让朋友重新创建", false)
		return
	}
	if errors.Is(err, room.ErrMemberNotFound) || errors.Is(err, room.ErrMemberNotActive) {
		c.sendRoomError(requestID, "member_not_active", "成员不在房间中", false)
		return
	}
	c.sendRoomError(requestID, "internal_error", "服务器错误", true)
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
		if !c.enqueuePrivate("heartbeat.ping", heartbeatMessagePayload{PingID: pingID, ServerTime: c.hub.currentTime()}) {
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
					c.closeWithMode(outbound.closeMode, outbound.closeCode, outbound.closeReason)
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
				c.closeWithMode(outbound.closeMode, outbound.closeCode, outbound.closeReason)
				return
			}
		}
	}
}

func (c *connection) enqueue(outbound outboundMessage) bool {
	if c.enqueueLocked(outbound) {
		return true
	}
	c.close(websocket.StatusPolicyViolation, "slow consumer")
	return false
}

func (c *connection) enqueueLocked(outbound outboundMessage) bool {
	select {
	case <-c.done:
		return false
	case c.send <- outbound:
		return true
	default:
		return false
	}
}

func (c *connection) enqueuePrivate(eventType string, payload any) bool {
	c.hub.mu.Lock()
	event := eventEnvelope{Type: eventType, Seq: c.hub.currentSeqLocked(c.roomID), SentAt: c.hub.currentTime(), Payload: payload}
	queued := c.enqueueLocked(outboundMessage{event: event})
	c.hub.mu.Unlock()
	if !queued {
		c.close(websocket.StatusPolicyViolation, "slow consumer")
	}
	return queued
}

func closeSlowClients(clients []*connection) {
	for _, client := range clients {
		client.close(websocket.StatusPolicyViolation, "slow consumer")
	}
}

func (h *Hub) newRoomErrorEvent(roomID string, requestID *string, code string, message string, retryable bool) eventEnvelope {
	payload := errorMessagePayload{RequestID: requestID, Retryable: retryable}
	payload.Error.Code = code
	payload.Error.Message = message
	return h.newPrivateEvent(roomID, "room.error", payload)
}

func (c *connection) sendRoomError(requestID *string, code string, message string, retryable bool) {
	payload := errorMessagePayload{RequestID: requestID, Retryable: retryable}
	payload.Error.Code = code
	payload.Error.Message = message
	c.enqueuePrivate("room.error", payload)
}

func (c *connection) close(code websocket.StatusCode, reason string) {
	c.closeWithMode(closeModeReconnect, code, reason)
}

func (c *connection) closeWithMode(mode closeMode, code websocket.StatusCode, reason string) {
	c.once.Do(func() {
		c.cancel()
		_ = c.ws.Close(code, reason)
		if mode == closeModeReconnect {
			c.hub.handleUnexpectedDisconnect(c)
		} else {
			c.hub.unregister(c)
		}
		close(c.done)
	})
}

func (h *Hub) handleUnexpectedDisconnect(client *connection) {
	h.mu.Lock()
	roomState := h.rooms[client.roomID]
	if roomState == nil {
		h.mu.Unlock()
		return
	}
	h.removeConnectionLocked(roomState, client)
	if roomState.byMember[client.memberID] != nil {
		h.pruneRoomStateLocked(client.roomID, roomState)
		h.mu.Unlock()
		return
	}

	speakingResult, err := h.stateMutator.UpdateMemberSpeakingContext(context.Background(), room.UpdateMemberSpeakingInput{
		RoomID:   client.roomID,
		MemberID: client.memberID,
		Speaking: false,
	})
	if err != nil && isStableDisconnectError(err) {
		h.pruneRoomStateLocked(client.roomID, roomState)
		h.mu.Unlock()
		return
	}
	speakingChanged := err != nil || speakingResult.Changed
	if existing, ok := roomState.reconnecting[client.memberID]; ok && existing.timer != nil {
		existing.timer.Stop()
	}
	generation := h.reconnectCounter.Add(1)
	now := h.currentTime()
	deadline := now.Add(h.reconnectWindow)
	reconnecting := reconnectingMember{deadline: deadline, generation: generation}
	reconnecting.timer = time.AfterFunc(h.reconnectWindow, func() {
		h.handleReconnectTimeout(client.roomID, client.memberID, generation)
	})
	roomState.reconnecting[client.memberID] = reconnecting

	var slowClients []*connection
	if len(roomState.connections) > 0 {
		if speakingChanged {
			roomState.lastSpeakingAccepted[client.memberID] = now
			roomState.seq++
			event := eventEnvelope{
				Type:   "member.speaking_changed",
				Seq:    roomState.seq,
				SentAt: now,
				Payload: speakingChangedMessagePayload{
					MemberID:  client.memberID,
					Speaking:  false,
					ChangedAt: now,
				},
			}
			for candidate := range roomState.connections {
				if !candidate.enqueueLocked(outboundMessage{event: event}) {
					slowClients = append(slowClients, candidate)
				}
			}
		}
		roomState.seq++
		event := eventEnvelope{
			Type:   "member.reconnecting",
			Seq:    roomState.seq,
			SentAt: now,
			Payload: reconnectingMessagePayload{
				MemberID:          client.memberID,
				ReconnectUntil:    deadline,
				ReconnectWindowMS: durationMillis(h.reconnectWindow),
			},
		}
		for candidate := range roomState.connections {
			if !candidate.enqueueLocked(outboundMessage{event: event}) {
				slowClients = append(slowClients, candidate)
			}
		}
	}
	h.pruneRoomStateLocked(client.roomID, roomState)
	h.mu.Unlock()
	closeSlowClients(slowClients)
}

func (h *Hub) handleReconnectTimeout(roomID string, memberID string, generation int64) {
	h.mu.Lock()
	roomState := h.rooms[roomID]
	if roomState == nil {
		h.mu.Unlock()
		return
	}
	reconnecting, ok := roomState.reconnecting[memberID]
	if !ok || reconnecting.generation != generation {
		h.mu.Unlock()
		return
	}
	if roomState.byMember[memberID] != nil {
		delete(roomState.reconnecting, memberID)
		h.pruneRoomStateLocked(roomID, roomState)
		h.mu.Unlock()
		return
	}

	_, err := h.stateMutator.DisconnectMemberContext(context.Background(), room.DisconnectMemberInput{RoomID: roomID, MemberID: memberID})
	if err != nil && !isStableDisconnectError(err) && reconnecting.retries == 0 {
		reconnecting.retries = 1
		reconnecting.timer = time.AfterFunc(100*time.Millisecond, func() {
			h.handleReconnectTimeout(roomID, memberID, generation)
		})
		roomState.reconnecting[memberID] = reconnecting
		h.mu.Unlock()
		return
	}
	if err != nil {
		delete(roomState.reconnecting, memberID)
		h.pruneRoomStateLocked(roomID, roomState)
		h.mu.Unlock()
		return
	}

	delete(roomState.reconnecting, memberID)
	now := h.currentTime()
	var slowClients []*connection
	if len(roomState.connections) > 0 {
		roomState.seq++
		event := eventEnvelope{
			Type:   "member.disconnected",
			Seq:    roomState.seq,
			SentAt: now,
			Payload: disconnectedMessagePayload{
				MemberID:       memberID,
				DisconnectedAt: now,
				Reason:         "reconnect_timeout",
			},
		}
		for candidate := range roomState.connections {
			if !candidate.enqueueLocked(outboundMessage{event: event}) {
				slowClients = append(slowClients, candidate)
			}
		}
	}
	h.pruneRoomStateLocked(roomID, roomState)
	h.mu.Unlock()
	closeSlowClients(slowClients)
}

func isStableDisconnectError(err error) bool {
	return errors.Is(err, room.ErrRoomNotFound) || errors.Is(err, room.ErrRoomExpired) || errors.Is(err, room.ErrMemberNotFound) || errors.Is(err, room.ErrMemberNotActive)
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

func projectMembers(members []domain.Member, selfMemberID string, reconnecting map[string]reconnectingMember, ignoreReconnectMemberID string) []memberProjection {
	projections := make([]memberProjection, 0, len(members))
	for _, member := range members {
		var reconnectUntil *time.Time
		if reconnecting != nil && member.ID != ignoreReconnectMemberID {
			if reconnectingMember, ok := reconnecting[member.ID]; ok {
				deadline := reconnectingMember.deadline
				reconnectUntil = &deadline
				member.State = domain.MemberStateReconnecting
				member.Speaking = false
			}
		}
		projections = append(projections, projectMember(member, selfMemberID, reconnectUntil))
	}
	return projections
}

func projectMember(member domain.Member, selfMemberID string, reconnectUntil *time.Time) memberProjection {
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
		ReconnectUntil: reconnectUntil,
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
