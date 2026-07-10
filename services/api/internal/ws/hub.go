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
	reconnectRetryDelay        = 100 * time.Millisecond
	reconnectMutationTimeout   = 5 * time.Second
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
	afterFunc        func(time.Duration, func()) reconnectTimer
}

type reconnectTimer interface {
	Stop() bool
}

type reconnectPhase uint8

const (
	reconnectPhasePending reconnectPhase = iota
	reconnectPhaseRestorable
	reconnectPhaseTimingOut
)

type reconnectingMember struct {
	deadline        time.Time
	generation      int64
	timer           reconnectTimer
	phase           reconnectPhase
	speakingCleared bool
}

type roomConnections struct {
	transition           sync.Mutex
	refs                 int
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
	send         chan outboundGroup
	pong         chan string
	done         chan struct{}
	once         sync.Once
	lastResyncAt time.Time
}

type outboundGroup struct {
	events      []eventEnvelope
	closeAfter  bool
	closeCode   websocket.StatusCode
	closeReason string
	closeMode   closeMode
}

func newOutboundGroup(events ...eventEnvelope) outboundGroup {
	return outboundGroup{events: events}
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
		afterFunc: func(delay time.Duration, callback func()) reconnectTimer {
			return time.AfterFunc(delay, callback)
		},
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
	_, err = h.authorizer.AuthorizeMemberContext(r.Context(), room.AuthorizeMemberInput{RoomID: claims.RoomID, MemberID: claims.MemberID})
	if err != nil {
		writeAuthorizeHTTPError(w, err)
		return
	}

	wsConn, err := websocket.Accept(upgradeResponseWriter(w), r, &websocket.AcceptOptions{OriginPatterns: h.originPatterns})
	if err != nil {
		return
	}
	client := h.newConnection(wsConn, claims.RoomID, claims.MemberID)
	if err := h.registerWithInitialSnapshot(client); err != nil {
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
	roomState, release := h.acquireRoomState(roomValue.ID, false)
	if roomState == nil {
		return
	}
	now := h.currentTime()
	var slowClients []*connection
	h.mu.Lock()
	if len(roomState.connections) == 0 {
		h.mu.Unlock()
		release()
		return
	}
	roomState.seq++
	event := eventEnvelope{Type: "member.joined", Seq: roomState.seq, SentAt: now, Payload: joinedMessagePayload{Member: projectMember(memberValue, "", nil)}}
	for client := range roomState.connections {
		if !client.enqueueLocked(newOutboundGroup(event)) {
			slowClients = append(slowClients, client)
		}
	}
	h.mu.Unlock()
	release()
	closeSlowClients(slowClients)
}

func (h *Hub) NotifyMemberLeft(_ context.Context, roomValue domain.Room, memberValue domain.Member) {
	if h == nil || strings.TrimSpace(roomValue.ID) == "" || strings.TrimSpace(memberValue.ID) == "" {
		return
	}
	roomState, release := h.acquireRoomState(roomValue.ID, false)
	if roomState == nil {
		return
	}
	now := h.currentTime()
	var slowClients []*connection
	var leavingSlow bool
	var leaving *connection
	h.mu.Lock()
	if reconnecting, ok := roomState.reconnecting[memberValue.ID]; ok {
		if reconnecting.timer != nil {
			reconnecting.timer.Stop()
		}
		delete(roomState.reconnecting, memberValue.ID)
	}
	delete(roomState.lastSpeakingAccepted, memberValue.ID)
	if len(roomState.connections) == 0 {
		h.mu.Unlock()
		release()
		return
	}
	roomState.seq++
	event := eventEnvelope{Type: "member.left", Seq: roomState.seq, SentAt: now, Payload: leftMessagePayload{MemberID: memberValue.ID, LeftAt: now}}
	leaving = roomState.byMember[memberValue.ID]
	for client := range roomState.connections {
		outbound := newOutboundGroup(event)
		if client == leaving {
			outbound.closeAfter = true
			outbound.closeCode = websocket.StatusNormalClosure
			outbound.closeReason = "member left"
			outbound.closeMode = closeModeNoReconnect
		}
		if !client.enqueueLocked(outbound) {
			if client == leaving {
				leavingSlow = true
			} else {
				slowClients = append(slowClients, client)
			}
		}
	}
	if leaving != nil {
		h.removeConnectionLocked(roomState, leaving)
	}
	h.mu.Unlock()
	release()
	if leavingSlow {
		leaving.closeWithMode(closeModeNoReconnect, websocket.StatusPolicyViolation, "slow consumer")
	}
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
		send:     make(chan outboundGroup, h.connectionQueueSize),
		pong:     make(chan string, 4),
		done:     make(chan struct{}),
	}
}

func (h *Hub) registerWithInitialSnapshot(client *connection) error {
	roomState, release := h.acquireRoomState(client.roomID, true)
	var old *connection
	var slowClients []*connection

	now := h.currentTime()
	h.mu.Lock()
	sharedSeq := roomState.seq
	reconnecting, restoring := roomState.reconnecting[client.memberID]
	if restoring && (reconnecting.phase != reconnectPhaseRestorable || !now.Before(reconnecting.deadline)) {
		h.mu.Unlock()
		release()
		return errors.New("member reconnect is not restorable")
	}
	reconnectingSnapshot := copyReconnectingMembers(roomState.reconnecting)
	h.mu.Unlock()

	snapshot, err := h.buildSnapshotEventAtSeq(client.ctx, client.roomID, client.memberID, sharedSeq, client.memberID, reconnectingSnapshot)
	if err != nil {
		release()
		return err
	}
	authorized, err := h.authorizer.AuthorizeMemberContext(client.ctx, room.AuthorizeMemberInput{RoomID: client.roomID, MemberID: client.memberID})
	if err != nil {
		release()
		return err
	}
	if authorized.Room.ID != client.roomID || authorized.Member.ID != client.memberID {
		release()
		return errors.New("authorized member identity changed during registration")
	}

	now = h.currentTime()
	h.mu.Lock()
	reconnecting, restoring = roomState.reconnecting[client.memberID]
	if restoring && (reconnecting.phase != reconnectPhaseRestorable || !now.Before(reconnecting.deadline)) {
		h.mu.Unlock()
		release()
		return errors.New("member reconnect is not restorable")
	}
	initialEvents := []eventEnvelope{snapshot}
	restoredSeq := roomState.seq
	if restoring {
		restoredSeq++
		initialEvents = append(initialEvents, eventEnvelope{
			Type:   "member.restored",
			Seq:    restoredSeq,
			SentAt: now,
			Payload: restoredMessagePayload{
				Member:     projectMember(authorized.Member, client.memberID, nil),
				RestoredAt: now,
			},
		})
	}
	if client.ctx.Err() != nil || !client.enqueueLocked(newOutboundGroup(initialEvents...)) {
		h.mu.Unlock()
		release()
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
		if reconnecting.timer != nil {
			reconnecting.timer.Stop()
		}
		roomState.seq = restoredSeq
		for candidate := range roomState.connections {
			if candidate == client {
				continue
			}
			restoredEvent := eventEnvelope{
				Type:   "member.restored",
				Seq:    restoredSeq,
				SentAt: now,
				Payload: restoredMessagePayload{
					Member:     projectMember(authorized.Member, candidate.memberID, nil),
					RestoredAt: now,
				},
			}
			if !candidate.enqueueLocked(newOutboundGroup(restoredEvent)) {
				slowClients = append(slowClients, candidate)
			}
		}
		delete(roomState.reconnecting, client.memberID)
	}
	h.mu.Unlock()
	release()
	if old != nil {
		old.closeWithMode(closeModeNoReconnect, websocket.StatusNormalClosure, "connection replaced")
	}
	closeSlowClients(slowClients)
	return nil
}

func (h *Hub) unregister(client *connection) {
	roomState, release := h.acquireRoomState(client.roomID, false)
	if roomState == nil {
		return
	}
	h.mu.Lock()
	h.removeConnectionLocked(roomState, client)
	h.mu.Unlock()
	release()
}

func connectionIsCurrentLocked(roomState *roomConnections, client *connection) bool {
	return roomState != nil && client != nil && client.ctx.Err() == nil && roomState.byMember[client.memberID] == client
}

func (h *Hub) removeConnectionLocked(roomState *roomConnections, client *connection) {
	delete(roomState.connections, client)
	if roomState.byMember[client.memberID] == client {
		delete(roomState.byMember, client.memberID)
	}
}

func (h *Hub) pruneRoomStateLocked(roomID string, roomState *roomConnections) {
	if roomState.refs == 0 && len(roomState.connections) == 0 && len(roomState.reconnecting) == 0 {
		delete(h.rooms, roomID)
	}
}

// acquireRoomState pins the room before waiting for its transition gate.
// Callers may briefly take h.mu while holding the gate, but never run external I/O under h.mu.
func (h *Hub) acquireRoomState(roomID string, create bool) (*roomConnections, func()) {
	h.mu.Lock()
	roomState := h.rooms[roomID]
	if roomState == nil && create {
		roomState = h.roomStateLocked(roomID)
	}
	if roomState == nil {
		h.mu.Unlock()
		return nil, func() {}
	}
	roomState.refs++
	h.mu.Unlock()

	roomState.transition.Lock()
	return roomState, func() {
		roomState.transition.Unlock()
		h.mu.Lock()
		roomState.refs--
		h.pruneRoomStateLocked(roomID, roomState)
		h.mu.Unlock()
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
	roomState, release := c.hub.acquireRoomState(c.roomID, false)
	if roomState == nil {
		c.sendRoomError(nil, "member_not_active", "成员不在房间中", false)
		return
	}
	c.hub.mu.Lock()
	if !connectionIsCurrentLocked(roomState, c) {
		c.hub.mu.Unlock()
		release()
		return
	}
	seq := roomState.seq
	reconnecting := copyReconnectingMembers(roomState.reconnecting)
	c.hub.mu.Unlock()

	event, err := c.hub.buildSnapshotEventAtSeq(c.ctx, c.roomID, c.memberID, seq, "", reconnecting)
	if err != nil {
		errorEvent := c.hub.newRoomErrorEvent(c.roomID, nil, "internal_error", "服务器错误", true)
		release()
		c.enqueue(outboundGroup{
			events:      []eventEnvelope{errorEvent},
			closeAfter:  true,
			closeCode:   websocket.StatusInternalError,
			closeReason: "snapshot failed",
		})
		return
	}
	c.hub.mu.Lock()
	slowClient := connectionIsCurrentLocked(roomState, c) && !c.enqueueLocked(newOutboundGroup(event))
	c.hub.mu.Unlock()
	release()
	if slowClient {
		c.close(websocket.StatusPolicyViolation, "slow consumer")
	}
}

func (h *Hub) buildSnapshotEvent(ctx context.Context, roomID string, selfMemberID string) (eventEnvelope, error) {
	roomState, release := h.acquireRoomState(roomID, true)
	h.mu.Lock()
	seq := roomState.seq
	reconnecting := copyReconnectingMembers(roomState.reconnecting)
	h.mu.Unlock()
	event, err := h.buildSnapshotEventAtSeq(ctx, roomID, selfMemberID, seq, "", reconnecting)
	release()
	return event, err
}

func (h *Hub) buildSnapshotEventAtSeq(ctx context.Context, roomID string, selfMemberID string, seq int64, ignoreReconnectMemberID string, reconnecting map[string]reconnectingMember) (eventEnvelope, error) {
	roomValue, err := h.snapshotStore.FindRoomByID(ctx, roomID)
	if err != nil {
		return eventEnvelope{}, err
	}
	members, err := h.snapshotStore.ListRoomMembersByStates(ctx, roomID, activeMemberStates())
	if err != nil {
		return eventEnvelope{}, err
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

func copyReconnectingMembers(source map[string]reconnectingMember) map[string]reconnectingMember {
	if len(source) == 0 {
		return nil
	}
	copied := make(map[string]reconnectingMember, len(source))
	for memberID, reconnecting := range source {
		copied[memberID] = reconnecting
	}
	return copied
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
	roomState, release := c.hub.acquireRoomState(c.roomID, false)
	if roomState == nil {
		return
	}
	c.hub.mu.Lock()
	current := connectionIsCurrentLocked(roomState, c)
	c.hub.mu.Unlock()
	if !current {
		release()
		return
	}

	result, err := c.hub.stateMutator.UpdateMemberMuteContext(c.ctx, room.UpdateMemberMuteInput{
		RoomID:   c.roomID,
		MemberID: c.memberID,
		Muted:    *payload.Muted,
	})
	if err != nil {
		release()
		c.sendStateMutationError(requestID, err)
		return
	}
	if !result.MutedChanged && !result.SpeakingChanged {
		release()
		return
	}

	now := c.hub.currentTime()
	var slowClients []*connection
	c.hub.mu.Lock()
	if connectionIsCurrentLocked(roomState, c) && len(roomState.connections) > 0 {
		events := make([]eventEnvelope, 0, 2)
		if result.SpeakingChanged {
			delete(roomState.lastSpeakingAccepted, c.memberID)
			roomState.seq++
			events = append(events, eventEnvelope{
				Type:   "member.speaking_changed",
				Seq:    roomState.seq,
				SentAt: now,
				Payload: speakingChangedMessagePayload{
					MemberID:  result.Member.ID,
					Speaking:  result.Member.Speaking,
					ChangedAt: now,
				},
			})
		}
		if result.MutedChanged {
			roomState.seq++
			events = append(events, eventEnvelope{
				Type:   "member.muted_changed",
				Seq:    roomState.seq,
				SentAt: now,
				Payload: mutedChangedMessagePayload{
					MemberID:  result.Member.ID,
					Muted:     result.Member.Muted,
					ChangedAt: now,
				},
			})
		}
		for client := range roomState.connections {
			if !client.enqueueLocked(newOutboundGroup(events...)) {
				slowClients = append(slowClients, client)
			}
		}
	}
	c.hub.mu.Unlock()
	release()
	closeSlowClients(slowClients)
}

func (c *connection) handleSpeakingChanged(raw json.RawMessage, requestID *string) {
	var payload speakingCommandPayload
	if len(raw) == 0 || json.Unmarshal(raw, &payload) != nil || payload.Speaking == nil {
		c.sendRoomError(requestID, "invalid_message", "消息格式无效", false)
		return
	}
	roomState, release := c.hub.acquireRoomState(c.roomID, false)
	if roomState == nil {
		return
	}
	now := c.hub.currentTime()
	c.hub.mu.Lock()
	current := connectionIsCurrentLocked(roomState, c)
	lastAccepted := roomState.lastSpeakingAccepted[c.memberID]
	throttled := *payload.Speaking && c.hub.speakingMinInterval > 0 && !lastAccepted.IsZero() && now.Sub(lastAccepted) < c.hub.speakingMinInterval
	c.hub.mu.Unlock()
	if !current || throttled {
		release()
		return
	}

	result, err := c.hub.stateMutator.UpdateMemberSpeakingContext(c.ctx, room.UpdateMemberSpeakingInput{
		RoomID:   c.roomID,
		MemberID: c.memberID,
		Speaking: *payload.Speaking,
	})
	if err != nil {
		release()
		c.sendStateMutationError(requestID, err)
		return
	}
	if !result.Changed {
		release()
		return
	}

	var slowClients []*connection
	c.hub.mu.Lock()
	if connectionIsCurrentLocked(roomState, c) && len(roomState.connections) > 0 {
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
			if !client.enqueueLocked(newOutboundGroup(event)) {
				slowClients = append(slowClients, client)
			}
		}
	}
	c.hub.mu.Unlock()
	release()
	closeSlowClients(slowClients)
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
		default:
		}
		select {
		case <-c.done:
			return
		case outbound := <-c.send:
			select {
			case <-c.done:
				return
			default:
			}
			for _, event := range outbound.events {
				if event.Type == "" {
					continue
				}
				ctx, cancel := context.WithTimeout(c.ctx, c.hub.writeTimeout)
				err := wsjson.Write(ctx, c.ws, event)
				cancel()
				if err != nil {
					c.close(websocket.StatusInternalError, "write failed")
					return
				}
			}
			if outbound.closeAfter {
				c.closeWithMode(outbound.closeMode, outbound.closeCode, outbound.closeReason)
				return
			}
		}
	}
}

func (c *connection) enqueue(outbound outboundGroup) bool {
	if c.enqueueLocked(outbound) {
		return true
	}
	c.close(websocket.StatusPolicyViolation, "slow consumer")
	return false
}

func (c *connection) enqueueLocked(outbound outboundGroup) bool {
	if c.ctx.Err() != nil {
		return false
	}
	select {
	case <-c.done:
		return false
	default:
	}
	select {
	case c.send <- outbound:
		return true
	default:
		return false
	}
}

func (c *connection) enqueuePrivate(eventType string, payload any) bool {
	now := c.hub.currentTime()
	c.hub.mu.Lock()
	event := eventEnvelope{Type: eventType, Seq: c.hub.currentSeqLocked(c.roomID), SentAt: now, Payload: payload}
	queued := c.enqueueLocked(newOutboundGroup(event))
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
		close(c.done)
		if mode == closeModeReconnect {
			c.hub.handleUnexpectedDisconnect(c)
		} else {
			c.hub.unregister(c)
		}
		if c.ws != nil {
			_ = c.ws.Close(code, reason)
		}
	})
}

func (h *Hub) handleUnexpectedDisconnect(client *connection) {
	lostAt := h.currentTime()
	roomState, release := h.acquireRoomState(client.roomID, false)
	if roomState == nil {
		return
	}

	deadline := lostAt.Add(h.reconnectWindow)
	generation := h.reconnectCounter.Add(1)
	var previousTimer reconnectTimer
	h.mu.Lock()
	if roomState.byMember[client.memberID] != client {
		h.removeConnectionLocked(roomState, client)
		h.mu.Unlock()
		release()
		return
	}
	h.removeConnectionLocked(roomState, client)
	if existing, ok := roomState.reconnecting[client.memberID]; ok {
		previousTimer = existing.timer
	}
	roomState.reconnecting[client.memberID] = reconnectingMember{
		deadline:   deadline,
		generation: generation,
		phase:      reconnectPhasePending,
	}
	delete(roomState.lastSpeakingAccepted, client.memberID)
	h.mu.Unlock()
	if previousTimer != nil {
		previousTimer.Stop()
	}

	slowClients := h.advancePendingReconnect(roomState, client.roomID, client.memberID, generation)
	release()
	closeSlowClients(slowClients)
}

func (h *Hub) advancePendingReconnect(roomState *roomConnections, roomID string, memberID string, generation int64) []*connection {
	reconnecting, ok := h.reconnectingMemberForGeneration(roomState, memberID, generation)
	if !ok || reconnecting.phase != reconnectPhasePending {
		return nil
	}
	if !h.currentTime().Before(reconnecting.deadline) {
		if !h.transitionReconnectPhase(roomState, memberID, generation, reconnectPhasePending, reconnectPhaseTimingOut) {
			return nil
		}
		return h.completeReconnectTimeout(roomState, roomID, memberID, generation)
	}

	mutationContext, cancel := context.WithTimeout(context.Background(), reconnectMutationTimeout)
	speakingResult, err := h.stateMutator.UpdateMemberSpeakingContext(mutationContext, room.UpdateMemberSpeakingInput{
		RoomID:   roomID,
		MemberID: memberID,
		Speaking: false,
	})
	cancel()
	if err != nil {
		if isStableDisconnectError(err) {
			h.clearReconnectMember(roomState, memberID, generation)
			return nil
		}
		h.scheduleReconnect(roomState, roomID, memberID, generation, reconnectRetryDelayUntil(reconnecting.deadline, h.currentTime()))
		return nil
	}

	now := h.currentTime()
	if !now.Before(reconnecting.deadline) {
		if !h.transitionReconnectPhase(roomState, memberID, generation, reconnectPhasePending, reconnectPhaseTimingOut) {
			return nil
		}
		return h.completeReconnectTimeout(roomState, roomID, memberID, generation)
	}
	if !h.transitionReconnectPhase(roomState, memberID, generation, reconnectPhasePending, reconnectPhaseRestorable) {
		return nil
	}
	h.scheduleReconnect(roomState, roomID, memberID, generation, reconnecting.deadline.Sub(now))
	return h.broadcastReconnectAvailable(roomState, memberID, generation, reconnecting.deadline, speakingResult.Changed, now)
}

func (h *Hub) handleReconnectTimeout(roomID string, memberID string, generation int64) {
	roomState, release := h.acquireRoomState(roomID, false)
	if roomState == nil {
		return
	}

	h.mu.Lock()
	reconnecting, ok := roomState.reconnecting[memberID]
	if !ok || reconnecting.generation != generation {
		h.mu.Unlock()
		release()
		return
	}
	timer := reconnecting.timer
	reconnecting.timer = nil
	roomState.reconnecting[memberID] = reconnecting
	connected := roomState.byMember[memberID] != nil
	h.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
	if connected {
		h.clearReconnectMember(roomState, memberID, generation)
		release()
		return
	}

	var slowClients []*connection
	now := h.currentTime()
	switch reconnecting.phase {
	case reconnectPhasePending:
		if now.Before(reconnecting.deadline) {
			slowClients = h.advancePendingReconnect(roomState, roomID, memberID, generation)
			break
		}
		if h.transitionReconnectPhase(roomState, memberID, generation, reconnectPhasePending, reconnectPhaseTimingOut) {
			slowClients = h.completeReconnectTimeout(roomState, roomID, memberID, generation)
		}
	case reconnectPhaseRestorable:
		if now.Before(reconnecting.deadline) {
			h.scheduleReconnect(roomState, roomID, memberID, generation, reconnecting.deadline.Sub(now))
			break
		}
		if h.transitionReconnectPhase(roomState, memberID, generation, reconnectPhaseRestorable, reconnectPhaseTimingOut) {
			slowClients = h.completeReconnectTimeout(roomState, roomID, memberID, generation)
		}
	case reconnectPhaseTimingOut:
		slowClients = h.completeReconnectTimeout(roomState, roomID, memberID, generation)
	}
	release()
	closeSlowClients(slowClients)
}

func (h *Hub) completeReconnectTimeout(roomState *roomConnections, roomID string, memberID string, generation int64) []*connection {
	mutationContext, cancel := context.WithTimeout(context.Background(), reconnectMutationTimeout)
	result, err := h.stateMutator.DisconnectMemberContext(mutationContext, room.DisconnectMemberInput{RoomID: roomID, MemberID: memberID})
	cancel()
	if err != nil {
		if isStableDisconnectError(err) {
			h.clearReconnectMember(roomState, memberID, generation)
			return nil
		}
		h.scheduleReconnect(roomState, roomID, memberID, generation, reconnectRetryDelay)
		return nil
	}

	h.clearReconnectMember(roomState, memberID, generation)
	if !result.Transitioned {
		return nil
	}
	return h.broadcastReconnectDisconnected(roomState, memberID, h.currentTime())
}

func (h *Hub) reconnectingMemberForGeneration(roomState *roomConnections, memberID string, generation int64) (reconnectingMember, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	reconnecting, ok := roomState.reconnecting[memberID]
	return reconnecting, ok && reconnecting.generation == generation
}

func (h *Hub) transitionReconnectPhase(roomState *roomConnections, memberID string, generation int64, from reconnectPhase, to reconnectPhase) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	reconnecting, ok := roomState.reconnecting[memberID]
	if !ok || reconnecting.generation != generation || reconnecting.phase != from {
		return false
	}
	reconnecting.phase = to
	if to == reconnectPhaseRestorable {
		reconnecting.speakingCleared = true
	}
	roomState.reconnecting[memberID] = reconnecting
	return true
}

func (h *Hub) scheduleReconnect(roomState *roomConnections, roomID string, memberID string, generation int64, delay time.Duration) {
	if delay < 0 {
		delay = 0
	}

	h.mu.Lock()
	reconnecting, ok := roomState.reconnecting[memberID]
	if !ok || reconnecting.generation != generation {
		h.mu.Unlock()
		return
	}
	previousTimer := reconnecting.timer
	reconnecting.timer = nil
	roomState.reconnecting[memberID] = reconnecting
	h.mu.Unlock()
	if previousTimer != nil {
		previousTimer.Stop()
	}

	afterFunc := h.afterFunc
	if afterFunc == nil {
		afterFunc = func(delay time.Duration, callback func()) reconnectTimer {
			return time.AfterFunc(delay, callback)
		}
	}
	timer := afterFunc(delay, func() {
		h.handleReconnectTimeout(roomID, memberID, generation)
	})

	h.mu.Lock()
	reconnecting, ok = roomState.reconnecting[memberID]
	if ok && reconnecting.generation == generation {
		reconnecting.timer = timer
		roomState.reconnecting[memberID] = reconnecting
	}
	h.mu.Unlock()
	if !ok || reconnecting.generation != generation {
		if timer != nil {
			timer.Stop()
		}
	}
}

func (h *Hub) clearReconnectMember(roomState *roomConnections, memberID string, generation int64) {
	var timer reconnectTimer
	h.mu.Lock()
	if reconnecting, ok := roomState.reconnecting[memberID]; ok && reconnecting.generation == generation {
		timer = reconnecting.timer
		delete(roomState.reconnecting, memberID)
		delete(roomState.lastSpeakingAccepted, memberID)
	}
	h.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}

func (h *Hub) broadcastReconnectAvailable(roomState *roomConnections, memberID string, generation int64, deadline time.Time, speakingChanged bool, changedAt time.Time) []*connection {
	var slowClients []*connection
	h.mu.Lock()
	reconnecting, ok := roomState.reconnecting[memberID]
	if !ok || reconnecting.generation != generation || reconnecting.phase != reconnectPhaseRestorable || len(roomState.connections) == 0 {
		h.mu.Unlock()
		return nil
	}
	events := make([]eventEnvelope, 0, 2)
	if speakingChanged {
		roomState.seq++
		events = append(events, eventEnvelope{
			Type:   "member.speaking_changed",
			Seq:    roomState.seq,
			SentAt: changedAt,
			Payload: speakingChangedMessagePayload{
				MemberID:  memberID,
				Speaking:  false,
				ChangedAt: changedAt,
			},
		})
	}
	roomState.seq++
	events = append(events, eventEnvelope{
		Type:   "member.reconnecting",
		Seq:    roomState.seq,
		SentAt: changedAt,
		Payload: reconnectingMessagePayload{
			MemberID:          memberID,
			ReconnectUntil:    deadline,
			ReconnectWindowMS: durationMillis(h.reconnectWindow),
		},
	})
	for candidate := range roomState.connections {
		if !candidate.enqueueLocked(newOutboundGroup(events...)) {
			slowClients = append(slowClients, candidate)
		}
	}
	h.mu.Unlock()
	return slowClients
}

func (h *Hub) broadcastReconnectDisconnected(roomState *roomConnections, memberID string, disconnectedAt time.Time) []*connection {
	var slowClients []*connection
	h.mu.Lock()
	if len(roomState.connections) == 0 {
		h.mu.Unlock()
		return nil
	}
	roomState.seq++
	event := eventEnvelope{
		Type:   "member.disconnected",
		Seq:    roomState.seq,
		SentAt: disconnectedAt,
		Payload: disconnectedMessagePayload{
			MemberID:       memberID,
			DisconnectedAt: disconnectedAt,
			Reason:         "reconnect_timeout",
		},
	}
	for candidate := range roomState.connections {
		if !candidate.enqueueLocked(newOutboundGroup(event)) {
			slowClients = append(slowClients, candidate)
		}
	}
	h.mu.Unlock()
	return slowClients
}

func reconnectRetryDelayUntil(deadline time.Time, now time.Time) time.Duration {
	remaining := deadline.Sub(now)
	if remaining <= 0 {
		return 0
	}
	if remaining < reconnectRetryDelay {
		return remaining
	}
	return reconnectRetryDelay
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

func (m reconnectingMember) projectsAsReconnecting() bool {
	return m.phase == reconnectPhaseRestorable || m.phase == reconnectPhaseTimingOut && m.speakingCleared
}

func projectMembers(members []domain.Member, selfMemberID string, reconnecting map[string]reconnectingMember, ignoreReconnectMemberID string) []memberProjection {
	projections := make([]memberProjection, 0, len(members))
	for _, member := range members {
		var reconnectUntil *time.Time
		if reconnecting != nil && member.ID != ignoreReconnectMemberID {
			if reconnectingMember, ok := reconnecting[member.ID]; ok && reconnectingMember.projectsAsReconnecting() {
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
