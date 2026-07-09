package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"echo/services/api/internal/domain"
	httpapi "echo/services/api/internal/http"
	"echo/services/api/internal/invite"
	"echo/services/api/internal/room"
	"echo/services/api/internal/session"
	"echo/services/api/internal/store"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

var wsTestNow = time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

const wsTestSessionSecret = "ws-room-session-secret"

func TestValidConnectionReceivesImmediateSnapshot(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	joined := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	disconnected := wsTestMember("mem_disconnected_snapshot", created.Room.ID, domain.MemberStateDisconnected, false, wsTestNow.Add(30*time.Second))
	if err := integration.repository.CreateMember(context.Background(), disconnected); err != nil {
		t.Fatalf("CreateMember disconnected returned error: %v", err)
	}

	conn := dialRoomWebSocket(t, integration.server.URL, joined.Room.ID, joined.RoomSessionToken)
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	event := readEvent(t, conn)
	if event.Type != "room.snapshot" {
		t.Fatalf("first event type = %q, want room.snapshot", event.Type)
	}
	var payload snapshotPayload
	decodePayload(t, event.Payload, &payload)

	if payload.Room.RoomID != created.Room.ID || payload.Room.InviteCode != created.Room.InviteCode || payload.Room.State != "active" {
		t.Fatalf("snapshot room = %#v, want created active room", payload.Room)
	}
	if payload.SelfMemberID != joined.Member.ID {
		t.Fatalf("self_member_id = %q, want %q", payload.SelfMemberID, joined.Member.ID)
	}
	if payload.LastSeq != event.Seq {
		t.Fatalf("payload.last_seq = %d, want envelope seq %d", payload.LastSeq, event.Seq)
	}
	if payload.HeartbeatIntervalMS != 15000 || payload.HeartbeatTimeoutMS != 30000 || payload.ReconnectWindowMS != 30000 {
		t.Fatalf("heartbeat/reconnect ms = %d/%d/%d, want 15000/30000/30000", payload.HeartbeatIntervalMS, payload.HeartbeatTimeoutMS, payload.ReconnectWindowMS)
	}
	wantIDs := []string{created.Member.ID, joined.Member.ID}
	if len(payload.Members) != len(wantIDs) {
		t.Fatalf("snapshot member count = %d, want %d: %#v", len(payload.Members), len(wantIDs), payload.Members)
	}
	selfCount := 0
	for i, wantID := range wantIDs {
		member := payload.Members[i]
		if member.MemberID != wantID {
			t.Fatalf("snapshot member[%d].member_id = %q, want %q", i, member.MemberID, wantID)
		}
		if member.MemberID == disconnected.ID {
			t.Fatalf("snapshot included disconnected member: %#v", member)
		}
		if member.IsSelf {
			selfCount++
			if member.MemberID != joined.Member.ID {
				t.Fatalf("is_self set for %q, want only %q", member.MemberID, joined.Member.ID)
			}
		}
		if member.State != "online" || member.VoiceMode != "push_to_talk" || member.ReconnectUntil != nil {
			t.Fatalf("snapshot member projection = %#v, want active push_to_talk with null reconnect_until", member)
		}
	}
	if selfCount != 1 {
		t.Fatalf("is_self count = %d, want exactly 1", selfCount)
	}
}

func TestValidCrossOriginConnectionReceivesSnapshotWhenOriginAllowed(t *testing.T) {
	integration := newWSIntegration(t, func(config *Config) {
		config.OriginPatterns = []string{"https://client.example"}
	})
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")

	conn := dialRoomWebSocketWithOrigin(t, integration.server.URL, created.Room.ID, created.RoomSessionToken, "https://client.example")
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	assertEventType(t, readEvent(t, conn), "room.snapshot")
}

func TestConnectionReceivesRoomEventAfterSnapshotBuildStarts(t *testing.T) {
	roomValue := wsTestRoom("room_ordering", "ORDER1", domain.RoomStateActive, wsTestNow)
	selfMember := wsTestMember("mem_ordering_self", roomValue.ID, domain.MemberStateOnline, true, wsTestNow)
	joinedMember := wsTestMember("mem_ordering_joined", roomValue.ID, domain.MemberStateOnline, false, wsTestNow.Add(time.Minute))
	snapshotStore := newBlockingSnapshotStore(roomValue, []domain.Member{selfMember, joinedMember})
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: selfMember}},
		SnapshotStore:     snapshotStore,
		RoomSessionSecret: wsTestSessionSecret,
		Now:               func() time.Time { return wsTestNow },
		WriteTimeout:      time.Second,
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hub.ServeRoomHTTP(w, r, roomValue.ID)
	}))
	defer server.Close()
	token := signRoomSessionToken(t, roomValue.ID, selfMember.ID, wsTestNow, 2*time.Hour)
	conn := dialRoomWebSocket(t, server.URL, roomValue.ID, token)
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	<-snapshotStore.entered
	joinedNotified := make(chan struct{})
	go func() {
		hub.NotifyMemberJoined(context.Background(), roomValue, joinedMember)
		close(joinedNotified)
	}()
	snapshotStore.releaseSnapshot()

	assertEventType(t, readEvent(t, conn), "room.snapshot")
	joined := readEvent(t, conn)
	assertEventType(t, joined, "member.joined")
	var payload memberJoinedPayload
	decodePayload(t, joined.Payload, &payload)
	if payload.Member.MemberID != joinedMember.ID {
		t.Fatalf("member.joined member_id = %q, want %q", payload.Member.MemberID, joinedMember.ID)
	}
	<-joinedNotified
}

func TestHandshakeRejectsInvalidRoomSessionCredentials(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	expiredToken := signRoomSessionToken(t, created.Room.ID, created.Member.ID, wsTestNow.Add(-3*time.Hour), 2*time.Hour)
	unsupportedVersionToken, err := session.SignClaims(wsTestSessionSecret, session.Claims{
		Version:   session.CurrentVersion + 1,
		RoomID:    created.Room.ID,
		MemberID:  created.Member.ID,
		ExpiresAt: wsTestNow.Add(2 * time.Hour),
	})
	if err != nil {
		t.Fatalf("session.SignClaims unsupported version returned error: %v", err)
	}

	tests := []struct {
		name        string
		roomID      string
		token       string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{name: "missing token", roomID: created.Room.ID, token: "", wantStatus: http.StatusUnauthorized, wantCode: "invalid_room_session", wantMessage: "连接凭证无效，请重新进入房间"},
		{name: "malformed token", roomID: created.Room.ID, token: "not-a-room-session", wantStatus: http.StatusUnauthorized, wantCode: "invalid_room_session", wantMessage: "连接凭证无效，请重新进入房间"},
		{name: "tampered token", roomID: created.Room.ID, token: "A" + created.RoomSessionToken[1:], wantStatus: http.StatusUnauthorized, wantCode: "invalid_room_session", wantMessage: "连接凭证无效，请重新进入房间"},
		{name: "expired token", roomID: created.Room.ID, token: expiredToken, wantStatus: http.StatusUnauthorized, wantCode: "room_session_expired", wantMessage: "连接凭证已过期，请重新进入房间"},
		{name: "unsupported version token", roomID: created.Room.ID, token: unsupportedVersionToken, wantStatus: http.StatusUnauthorized, wantCode: "invalid_room_session", wantMessage: "连接凭证无效，请重新进入房间"},
		{name: "room mismatch", roomID: "room_other", token: created.RoomSessionToken, wantStatus: http.StatusForbidden, wantCode: "room_session_mismatch", wantMessage: "连接凭证与房间不匹配"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, response, err := dialRoomWebSocketRaw(t, integration.server.URL, tt.roomID, tt.token)
			if err == nil {
				t.Fatalf("Dial succeeded, want pre-upgrade rejection")
			}
			assertPreUpgradeError(t, response, tt.wantStatus, tt.wantCode, tt.wantMessage)
		})
	}
}

func TestHandshakeRejectsInactiveOrMissingProductState(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	missingRoomToken := signRoomSessionToken(t, "room_missing_ws", "mem_missing_ws", wsTestNow, 2*time.Hour)
	missingMemberToken := signRoomSessionToken(t, created.Room.ID, "mem_missing_ws", wsTestNow, 2*time.Hour)
	expiredRoom := wsTestRoom("room_expired_ws", "EXPWS1", domain.RoomStateExpired, wsTestNow)
	expiredMember := wsTestMember("mem_expired_ws", expiredRoom.ID, domain.MemberStateOnline, true, wsTestNow)
	if err := integration.repository.CreateRoomWithMember(context.Background(), expiredRoom, expiredMember); err != nil {
		t.Fatalf("CreateRoomWithMember expired returned error: %v", err)
	}
	expiredRoomToken := signRoomSessionToken(t, expiredRoom.ID, expiredMember.ID, wsTestNow, 2*time.Hour)

	leaveResponse := performAuthorizedWSJSONRequest(t, integration.router, http.MethodPost, "/v1/rooms/"+created.Room.ID+"/leave", created.RoomSessionToken, map[string]string{"member_id": created.Member.ID})
	if leaveResponse.Code != http.StatusNoContent {
		t.Fatalf("leave status = %d, want 204, body: %s", leaveResponse.Code, leaveResponse.Body.String())
	}

	tests := []struct {
		name        string
		roomID      string
		token       string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{name: "missing room", roomID: "room_missing_ws", token: missingRoomToken, wantStatus: http.StatusNotFound, wantCode: "room_not_found", wantMessage: "房间不存在或已失效"},
		{name: "expired room", roomID: expiredRoom.ID, token: expiredRoomToken, wantStatus: http.StatusGone, wantCode: "room_expired", wantMessage: "该房间已过期，请让朋友重新创建"},
		{name: "missing member", roomID: created.Room.ID, token: missingMemberToken, wantStatus: http.StatusForbidden, wantCode: "member_not_active", wantMessage: "成员不在房间中"},
		{name: "disconnected member", roomID: created.Room.ID, token: created.RoomSessionToken, wantStatus: http.StatusForbidden, wantCode: "member_not_active", wantMessage: "成员不在房间中"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, response, err := dialRoomWebSocketRaw(t, integration.server.URL, tt.roomID, tt.token)
			if err == nil {
				t.Fatalf("Dial succeeded, want pre-upgrade rejection")
			}
			assertPreUpgradeError(t, response, tt.wantStatus, tt.wantCode, tt.wantMessage)
		})
	}
}

func TestHTTPJoinBroadcastsMemberJoinedToExistingClients(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	bob := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, bob.Room.ID, bob.RoomSessionToken)
	defer bobConn.Close(websocket.StatusNormalClosure, "test done")
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")

	carol := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Carol", "avatar_09")
	for name, conn := range map[string]*websocket.Conn{"host": hostConn, "bob": bobConn} {
		event := readEvent(t, conn)
		assertEventType(t, event, "member.joined")
		var payload memberJoinedPayload
		decodePayload(t, event.Payload, &payload)
		if payload.Member.MemberID != carol.Member.ID || payload.Member.IsSelf {
			t.Fatalf("%s member.joined payload = %#v, want joined member with is_self false", name, payload)
		}
		if payload.Member.Nickname != "Carol" || payload.Member.State != "online" || payload.Member.VoiceMode != "push_to_talk" {
			t.Fatalf("%s member.joined member projection = %#v, want Carol online push_to_talk", name, payload.Member)
		}
	}
}

func TestHTTPLeaveBroadcastsMemberLeftAndSnapshotsExcludeLeavingMember(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	joined := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, joined.Room.ID, joined.RoomSessionToken)
	defer bobConn.Close(websocket.StatusNormalClosure, "test done")
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")

	leaveResponse := performAuthorizedWSJSONRequest(t, integration.router, http.MethodPost, "/v1/rooms/"+created.Room.ID+"/leave", joined.RoomSessionToken, map[string]string{"member_id": joined.Member.ID})
	if leaveResponse.Code != http.StatusNoContent {
		t.Fatalf("leave status = %d, want 204, body: %s", leaveResponse.Code, leaveResponse.Body.String())
	}

	event := readEvent(t, hostConn)
	assertEventType(t, event, "member.left")
	var left memberLeftPayload
	decodePayload(t, event.Payload, &left)
	if left.MemberID != joined.Member.ID || left.LeftAt.IsZero() {
		t.Fatalf("member.left payload = %#v, want leaving member and left_at", left)
	}
	bobLeft := readEvent(t, bobConn)
	assertEventType(t, bobLeft, "member.left")
	assertConnectionCloses(t, bobConn)

	writeClientCommand(t, hostConn, map[string]any{"type": "room.resync_requested", "payload": map[string]any{"reason": "test"}})
	resync := readEvent(t, hostConn)
	assertEventType(t, resync, "room.snapshot")
	var snapshot snapshotPayload
	decodePayload(t, resync.Payload, &snapshot)
	for _, member := range snapshot.Members {
		if member.MemberID == joined.Member.ID {
			t.Fatalf("resync snapshot included left member: %#v", snapshot.Members)
		}
	}
}

func TestPrivateSnapshotAndErrorDoNotAdvanceSharedBroadcastSequenceForOtherClients(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	bob := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, bob.Room.ID, bob.RoomSessionToken)
	defer bobConn.Close(websocket.StatusNormalClosure, "test done")
	hostSnapshotEvent := readEvent(t, hostConn)
	assertEventType(t, hostSnapshotEvent, "room.snapshot")
	var hostSnapshot snapshotPayload
	decodePayload(t, hostSnapshotEvent.Payload, &hostSnapshot)
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")

	writeClientCommand(t, bobConn, map[string]any{"type": "room.resync_requested", "request_id": "resync-private", "payload": map[string]any{"reason": "test"}})
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")
	writeClientCommand(t, bobConn, map[string]any{"type": "member.speaking_changed", "request_id": "error-private", "payload": map[string]any{"speaking": true}})
	assertEventType(t, readEvent(t, bobConn), "room.error")

	carol := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Carol", "avatar_09")
	joined := readEvent(t, hostConn)
	assertEventType(t, joined, "member.joined")
	if joined.Seq != hostSnapshot.LastSeq+1 {
		t.Fatalf("member.joined seq = %d, want %d after private messages on another client", joined.Seq, hostSnapshot.LastSeq+1)
	}
	var payload memberJoinedPayload
	decodePayload(t, joined.Payload, &payload)
	if payload.Member.MemberID != carol.Member.ID {
		t.Fatalf("member.joined member_id = %q, want %q", payload.Member.MemberID, carol.Member.ID)
	}
}

func TestHeartbeatDoesNotAdvanceSharedBroadcastSequenceForOtherClients(t *testing.T) {
	integration := newWSIntegration(t, func(config *Config) {
		config.HeartbeatInterval = 10 * time.Millisecond
		config.HeartbeatTimeout = time.Second
	})
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	bob := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, bob.Room.ID, bob.RoomSessionToken)
	defer bobConn.Close(websocket.StatusNormalClosure, "test done")
	hostSnapshotEvent := readEvent(t, hostConn)
	assertEventType(t, hostSnapshotEvent, "room.snapshot")
	var hostSnapshot snapshotPayload
	decodePayload(t, hostSnapshotEvent.Payload, &hostSnapshot)
	bobSnapshotEvent := readEvent(t, bobConn)
	assertEventType(t, bobSnapshotEvent, "room.snapshot")
	var bobSnapshot snapshotPayload
	decodePayload(t, bobSnapshotEvent.Payload, &bobSnapshot)

	bobPing := readEvent(t, bobConn)
	assertEventType(t, bobPing, "heartbeat.ping")
	if bobPing.Seq != bobSnapshot.LastSeq {
		t.Fatalf("Bob heartbeat seq = %d, want current shared seq %d", bobPing.Seq, bobSnapshot.LastSeq)
	}
	var bobPingPayload heartbeatPingPayload
	decodePayload(t, bobPing.Payload, &bobPingPayload)
	writeClientCommand(t, bobConn, map[string]any{"type": "heartbeat.pong", "payload": map[string]string{"ping_id": bobPingPayload.PingID}})

	carol := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Carol", "avatar_09")
	joined := readUntilMemberJoinedRespondingToHeartbeat(t, hostConn, hostSnapshot.LastSeq)
	if joined.Seq != hostSnapshot.LastSeq+1 {
		t.Fatalf("member.joined seq = %d, want %d after private heartbeat on another client", joined.Seq, hostSnapshot.LastSeq+1)
	}
	var payload memberJoinedPayload
	decodePayload(t, joined.Payload, &payload)
	if payload.Member.MemberID != carol.Member.ID {
		t.Fatalf("member.joined member_id = %q, want %q", payload.Member.MemberID, carol.Member.ID)
	}
}

func TestHTTPLeaveRequiresMatchingRoomSessionBeforeBroadcastOrClose(t *testing.T) {
	integration := newWSIntegration(t)
	alice := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	bob := joinRoomThroughHTTP(t, integration.router, alice.Room.InviteCode, "Bob", "avatar_08")
	aliceConn := dialRoomWebSocket(t, integration.server.URL, alice.Room.ID, alice.RoomSessionToken)
	defer aliceConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, bob.Room.ID, bob.RoomSessionToken)
	defer bobConn.Close(websocket.StatusNormalClosure, "test done")
	assertEventType(t, readEvent(t, aliceConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")

	attackResponse := performAuthorizedWSJSONRequest(t, integration.router, http.MethodPost, "/v1/rooms/"+alice.Room.ID+"/leave", bob.RoomSessionToken, map[string]string{"member_id": alice.Member.ID})
	assertWSHTTPError(t, attackResponse, http.StatusForbidden, "room_session_mismatch", "连接凭证与房间不匹配")

	carol := joinRoomThroughHTTP(t, integration.router, alice.Room.InviteCode, "Carol", "avatar_09")
	joined := readEvent(t, aliceConn)
	assertEventType(t, joined, "member.joined")
	var payload memberJoinedPayload
	decodePayload(t, joined.Payload, &payload)
	if payload.Member.MemberID != carol.Member.ID {
		t.Fatalf("Alice connection received member %q, want later join %q", payload.Member.MemberID, carol.Member.ID)
	}
}

func TestHubPrunesEmptyRoomStateAndNotificationsWithoutClientsDoNotCreateRooms(t *testing.T) {
	integration := newWSIntegration(t)
	integration.hub.NotifyMemberJoined(context.Background(), domain.Room{ID: "room_without_clients"}, domain.Member{ID: "mem_without_clients"})
	if hubHasRoomState(integration.hub, "room_without_clients") {
		t.Fatalf("hub retained room state for HTTP-only notification without connected clients")
	}

	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	conn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	assertEventType(t, readEvent(t, conn), "room.snapshot")
	if !hubHasRoomState(integration.hub, created.Room.ID) {
		t.Fatalf("hub did not retain room state for connected room")
	}
	conn.Close(websocket.StatusNormalClosure, "test done")
	waitForHubRoomState(t, integration.hub, created.Room.ID, false)
}

func TestResyncRequestsAreRateLimitedPerConnection(t *testing.T) {
	integration := newWSIntegration(t, func(config *Config) {
		config.ResyncMinInterval = time.Minute
	})
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	conn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer conn.Close(websocket.StatusNormalClosure, "test done")
	assertEventType(t, readEvent(t, conn), "room.snapshot")

	for i := 0; i < 5; i++ {
		writeClientCommand(t, conn, map[string]any{"type": "room.resync_requested", "request_id": "resync-burst", "payload": map[string]any{"index": i}})
	}

	snapshots := 0
	rateLimited := 0
	for i := 0; i < 5; i++ {
		event := readEvent(t, conn)
		switch event.Type {
		case "room.snapshot":
			snapshots++
		case "room.error":
			var payload roomErrorPayload
			decodePayload(t, event.Payload, &payload)
			if payload.Error.Code == "rate_limited" && payload.Retryable {
				rateLimited++
			}
		default:
			t.Fatalf("burst response event type = %q, want room.snapshot or room.error", event.Type)
		}
	}
	if snapshots != 1 || rateLimited != 4 {
		t.Fatalf("resync burst responses snapshots/rate_limited = %d/%d, want 1/4", snapshots, rateLimited)
	}
}

func TestHeartbeatPingPongKeepsConnectionUsable(t *testing.T) {
	integration := newWSIntegration(t, func(config *Config) {
		config.HeartbeatInterval = 10 * time.Millisecond
		config.HeartbeatTimeout = 200 * time.Millisecond
	})
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	conn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer conn.Close(websocket.StatusNormalClosure, "test done")
	snapshotEvent := readEvent(t, conn)
	assertEventType(t, snapshotEvent, "room.snapshot")
	var snapshot snapshotPayload
	decodePayload(t, snapshotEvent.Payload, &snapshot)

	ping := readEvent(t, conn)
	assertEventType(t, ping, "heartbeat.ping")
	if ping.Seq != snapshot.LastSeq {
		t.Fatalf("heartbeat seq = %d, want current shared seq %d", ping.Seq, snapshot.LastSeq)
	}
	var pingPayload heartbeatPingPayload
	decodePayload(t, ping.Payload, &pingPayload)
	if pingPayload.PingID == "" || pingPayload.ServerTime.IsZero() {
		t.Fatalf("heartbeat ping payload = %#v, want ping_id and server_time", pingPayload)
	}
	writeClientCommand(t, conn, map[string]any{"type": "heartbeat.pong", "payload": map[string]string{"ping_id": pingPayload.PingID}})
	writeClientCommand(t, conn, map[string]any{"type": "room.resync_requested", "payload": map[string]any{"reason": "after_pong"}})

	for i := 0; i < 4; i++ {
		event := readEvent(t, conn)
		if event.Type == "room.snapshot" {
			return
		}
	}
	t.Fatalf("connection did not return a room.snapshot after heartbeat pong")
}

func TestHeartbeatTimeoutClosesConnectionWithoutProductStateMutation(t *testing.T) {
	integration := newWSIntegration(t, func(config *Config) {
		config.HeartbeatInterval = 10 * time.Millisecond
		config.HeartbeatTimeout = 25 * time.Millisecond
	})
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	conn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer conn.Close(websocket.StatusNormalClosure, "test done")
	assertEventType(t, readEvent(t, conn), "room.snapshot")
	assertEventType(t, readEvent(t, conn), "heartbeat.ping")

	assertConnectionCloses(t, conn)
	member, err := integration.repository.FindMemberByRoomAndID(context.Background(), created.Room.ID, created.Member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if member.State != domain.MemberStateOnline {
		t.Fatalf("member state after heartbeat timeout = %q, want online because Issue #15 does not write reconnect state", member.State)
	}
}

func TestUnknownAndInvalidMessagesReturnRoomErrorWithoutMutation(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	conn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer conn.Close(websocket.StatusNormalClosure, "test done")
	assertEventType(t, readEvent(t, conn), "room.snapshot")

	writeClientCommand(t, conn, map[string]any{"type": "member.speaking_changed", "request_id": "req-unknown", "payload": map[string]any{"speaking": true}})
	unknown := readEvent(t, conn)
	assertEventType(t, unknown, "room.error")
	var unknownPayload roomErrorPayload
	decodePayload(t, unknown.Payload, &unknownPayload)
	if unknownPayload.Error.Code != "unknown_message_type" || unknownPayload.RequestID == nil || *unknownPayload.RequestID != "req-unknown" {
		t.Fatalf("unknown message error payload = %#v, want unknown_message_type with request_id", unknownPayload)
	}

	writeRawClientMessage(t, conn, []byte(`{`))
	invalid := readEvent(t, conn)
	assertEventType(t, invalid, "room.error")
	var invalidPayload roomErrorPayload
	decodePayload(t, invalid.Payload, &invalidPayload)
	if invalidPayload.Error.Code != "invalid_message" {
		t.Fatalf("invalid message error payload = %#v, want invalid_message", invalidPayload)
	}

	member, err := integration.repository.FindMemberByRoomAndID(context.Background(), created.Room.ID, created.Member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if member.Speaking {
		t.Fatalf("member speaking after unsupported command = true, want false")
	}
}

type wsIntegration struct {
	router     http.Handler
	server     *httptest.Server
	repository *store.Repository
	hub        *Hub
}

func newWSIntegration(t *testing.T, options ...func(*Config)) *wsIntegration {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "echo.sqlite3"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	repository := store.NewRepository(db)
	roomService := room.NewService(repository, invite.NewGenerator())
	config := Config{
		Authorizer:          roomService,
		SnapshotStore:       repository,
		RoomSessionSecret:   wsTestSessionSecret,
		Now:                 func() time.Time { return wsTestNow },
		ReconnectWindow:     30 * time.Second,
		WriteTimeout:        time.Second,
		ConnectionQueueSize: 8,
	}
	for _, option := range options {
		option(&config)
	}
	hub := NewHub(config)
	router := httpapi.NewRouter(
		httpapi.WithRoomCreator(roomService),
		httpapi.WithRoomJoiner(roomService),
		httpapi.WithRoomLeaver(roomService),
		httpapi.WithRoomMemberAuthorizer(roomService),
		httpapi.WithCredentialConfig(httpapi.CredentialConfig{
			LiveKitURL:          "wss://livekit.test",
			LiveKitAPIKey:       "devkey",
			LiveKitAPISecret:    "devsecret",
			RoomSessionSecret:   wsTestSessionSecret,
			RoomSessionTokenTTL: 2 * time.Hour,
			LiveKitTokenTTL:     10 * time.Minute,
			Now:                 func() time.Time { return wsTestNow },
		}),
		httpapi.WithRoomWebSocket(hub),
		httpapi.WithRoomEventNotifier(hub),
	)
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	return &wsIntegration{router: router, server: server, repository: repository, hub: hub}
}

func createRoomThroughHTTP(t *testing.T, router http.Handler, nickname string, avatarID string) roomCredentialResponse {
	t.Helper()
	response := performWSJSONRequest(t, router, http.MethodPost, "/v1/rooms", map[string]string{
		"anonymous_id": "anon_" + strings.ToLower(nickname),
		"nickname":     nickname,
		"avatar_id":    avatarID,
		"room_name":    "今晚开黑",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want 201, body: %s", response.Code, response.Body.String())
	}
	return decodeRoomCredentialResponse(t, response)
}

func joinRoomThroughHTTP(t *testing.T, router http.Handler, inviteCode string, nickname string, avatarID string) roomCredentialResponse {
	t.Helper()
	response := performWSJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  inviteCode,
		"anonymous_id": "anon_" + strings.ToLower(nickname),
		"nickname":     nickname,
		"avatar_id":    avatarID,
	})
	if response.Code != http.StatusOK {
		t.Fatalf("POST /v1/rooms/join status = %d, want 200, body: %s", response.Code, response.Body.String())
	}
	return decodeRoomCredentialResponse(t, response)
}

func performWSJSONRequest(t *testing.T, router http.Handler, method string, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal payload: %v", err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func performAuthorizedWSJSONRequest(t *testing.T, router http.Handler, method string, target string, token string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal payload: %v", err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func dialRoomWebSocket(t *testing.T, serverURL string, roomID string, token string) *websocket.Conn {
	t.Helper()
	conn, response, err := dialRoomWebSocketRaw(t, serverURL, roomID, token)
	if err != nil {
		if response != nil {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("Dial returned error: %v, status=%d body=%s", err, response.StatusCode, string(body))
		}
		t.Fatalf("Dial returned error: %v", err)
	}
	return conn
}

func dialRoomWebSocketWithOrigin(t *testing.T, serverURL string, roomID string, token string, origin string) *websocket.Conn {
	t.Helper()
	conn, response, err := dialRoomWebSocketRawWithOptions(t, serverURL, roomID, token, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{origin}}})
	if err != nil {
		if response != nil {
			body, _ := io.ReadAll(response.Body)
			t.Fatalf("cross-origin Dial returned error: %v, status=%d body=%s", err, response.StatusCode, string(body))
		}
		t.Fatalf("cross-origin Dial returned error: %v", err)
	}
	return conn
}

func dialRoomWebSocketRaw(t *testing.T, serverURL string, roomID string, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	return dialRoomWebSocketRawWithOptions(t, serverURL, roomID, token, nil)
}

func dialRoomWebSocketRawWithOptions(t *testing.T, serverURL string, roomID string, token string, options *websocket.DialOptions) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/v1/rooms/" + url.PathEscape(roomID) + "/ws"
	if token != "" {
		wsURL += "?token=" + url.QueryEscape(token)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return websocket.Dial(ctx, wsURL, options)
}

func readEvent(t *testing.T, conn *websocket.Conn) testEvent {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var event testEvent
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("wsjson.Read returned error: %v", err)
	}
	if event.Seq < 0 || event.SentAt.IsZero() || len(event.Payload) == 0 {
		t.Fatalf("event envelope = %#v, want non-negative seq, sent_at, and payload", event)
	}
	return event
}

func assertConnectionCloses(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var event testEvent
	if err := wsjson.Read(ctx, conn, &event); err == nil {
		t.Fatalf("wsjson.Read succeeded with event %#v, want connection close", event)
	}
}

func writeClientCommand(t *testing.T, conn *websocket.Conn, message any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := wsjson.Write(ctx, conn, message); err != nil {
		t.Fatalf("wsjson.Write returned error: %v", err)
	}
}

func writeRawClientMessage(t *testing.T, conn *websocket.Conn, message []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, message); err != nil {
		t.Fatalf("Conn.Write returned error: %v", err)
	}
}

func decodePayload(t *testing.T, raw json.RawMessage, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("payload returned invalid JSON: %v; raw=%s", err, string(raw))
	}
}

func assertEventType(t *testing.T, event testEvent, want string) {
	t.Helper()
	if event.Type != want {
		t.Fatalf("event type = %q, want %q", event.Type, want)
	}
}

func readUntilMemberJoinedRespondingToHeartbeat(t *testing.T, conn *websocket.Conn, heartbeatSeq int64) testEvent {
	t.Helper()
	for i := 0; i < 10; i++ {
		event := readEvent(t, conn)
		switch event.Type {
		case "member.joined":
			return event
		case "heartbeat.ping":
			if event.Seq != heartbeatSeq {
				t.Fatalf("heartbeat before member.joined seq = %d, want %d", event.Seq, heartbeatSeq)
			}
			var payload heartbeatPingPayload
			decodePayload(t, event.Payload, &payload)
			writeClientCommand(t, conn, map[string]any{"type": "heartbeat.pong", "payload": map[string]string{"ping_id": payload.PingID}})
		default:
			t.Fatalf("event type = %q, want member.joined or heartbeat.ping", event.Type)
		}
	}
	t.Fatalf("did not receive member.joined before heartbeat retry limit")
	return testEvent{}
}

func assertWSHTTPError(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string, wantMessage string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, wantStatus, response.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("error response returned invalid JSON: %v", err)
	}
	if body.Error.Code != wantCode || body.Error.Message != wantMessage {
		t.Fatalf("error response = %s/%s, want %s/%s", body.Error.Code, body.Error.Message, wantCode, wantMessage)
	}
}

func hubHasRoomState(h *Hub, roomID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.rooms[roomID]
	return ok
}

func waitForHubRoomState(t *testing.T, h *Hub, roomID string, want bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if hubHasRoomState(h, roomID) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("hub room state for %q = %v, want %v", roomID, hubHasRoomState(h, roomID), want)
}

func assertPreUpgradeError(t *testing.T, response *http.Response, wantStatus int, wantCode string, wantMessage string) {
	t.Helper()
	if response == nil {
		t.Fatalf("response is nil, want HTTP error response")
	}
	defer response.Body.Close()
	if response.StatusCode != wantStatus {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("status = %d, want %d, body=%s", response.StatusCode, wantStatus, string(body))
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("error response returned invalid JSON: %v", err)
	}
	if body.Error.Code != wantCode || body.Error.Message != wantMessage {
		t.Fatalf("error response = %s/%s, want %s/%s", body.Error.Code, body.Error.Message, wantCode, wantMessage)
	}
}

func signRoomSessionToken(t *testing.T, roomID string, memberID string, now time.Time, ttl time.Duration) string {
	t.Helper()
	token, _, err := session.Sign(session.SignInput{Secret: wsTestSessionSecret, RoomID: roomID, MemberID: memberID, Now: now, TTL: ttl})
	if err != nil {
		t.Fatalf("session.Sign returned error: %v", err)
	}
	return token
}

type fixedAuthorizer struct {
	result room.AuthorizeMemberResult
	err    error
}

func (f fixedAuthorizer) AuthorizeMemberContext(context.Context, room.AuthorizeMemberInput) (room.AuthorizeMemberResult, error) {
	if f.err != nil {
		return room.AuthorizeMemberResult{}, f.err
	}
	return f.result, nil
}

type blockingSnapshotStore struct {
	room    domain.Room
	members []domain.Member
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingSnapshotStore(roomValue domain.Room, members []domain.Member) *blockingSnapshotStore {
	return &blockingSnapshotStore{room: roomValue, members: members, entered: make(chan struct{}), release: make(chan struct{})}
}

func (s *blockingSnapshotStore) FindRoomByID(context.Context, string) (domain.Room, error) {
	s.once.Do(func() { close(s.entered) })
	<-s.release
	return s.room, nil
}

func (s *blockingSnapshotStore) ListRoomMembersByStates(context.Context, string, []domain.MemberState) ([]domain.Member, error) {
	return append([]domain.Member(nil), s.members...), nil
}

func (s *blockingSnapshotStore) releaseSnapshot() {
	close(s.release)
}

func wsTestRoom(id string, inviteCode string, state domain.RoomState, now time.Time) domain.Room {
	return domain.Room{
		ID:              id,
		Name:            "今晚开黑",
		InviteCode:      inviteCode,
		LiveKitRoomName: "lk_" + id,
		HostAnonymousID: "anon_host",
		HostNickname:    "Alice",
		HostAvatarID:    "avatar_07",
		State:           state,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func wsTestMember(id string, roomID string, state domain.MemberState, isHost bool, joinedAt time.Time) domain.Member {
	return domain.Member{
		ID:              id,
		RoomID:          roomID,
		AnonymousID:     id + "_anon",
		Nickname:        "Member " + id,
		AvatarID:        "avatar_08",
		IsHost:          isHost,
		State:           state,
		Muted:           false,
		Speaking:        false,
		VoiceMode:       domain.VoiceModePushToTalk,
		JoinedAt:        joinedAt,
		LiveKitIdentity: id,
	}
}

type testEvent struct {
	Type    string          `json:"type"`
	Seq     int64           `json:"seq"`
	SentAt  time.Time       `json:"sent_at"`
	Payload json.RawMessage `json:"payload"`
}

type roomCredentialResponse struct {
	RoomSessionToken string `json:"room_session_token"`
	Room             struct {
		ID          string     `json:"id"`
		Name        string     `json:"name"`
		InviteCode  string     `json:"invite_code"`
		State       string     `json:"state"`
		CreatedAt   time.Time  `json:"created_at"`
		LastEmptyAt *time.Time `json:"last_empty_at"`
		ExpiresAt   *time.Time `json:"expires_at"`
	} `json:"room"`
	Member struct {
		ID        string    `json:"id"`
		Nickname  string    `json:"nickname"`
		AvatarID  string    `json:"avatar_id"`
		State     string    `json:"state"`
		JoinedAt  time.Time `json:"joined_at"`
		VoiceMode string    `json:"voice_mode"`
	} `json:"member"`
}

func decodeRoomCredentialResponse(t *testing.T, response *httptest.ResponseRecorder) roomCredentialResponse {
	t.Helper()
	var body roomCredentialResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response returned invalid JSON: %v", err)
	}
	return body
}

type snapshotPayload struct {
	Room struct {
		RoomID      string     `json:"room_id"`
		Name        string     `json:"name"`
		InviteCode  string     `json:"invite_code"`
		State       string     `json:"state"`
		CreatedAt   time.Time  `json:"created_at"`
		LastEmptyAt *time.Time `json:"last_empty_at"`
		ExpiresAt   *time.Time `json:"expires_at"`
	} `json:"room"`
	SelfMemberID        string         `json:"self_member_id"`
	Members             []memberObject `json:"members"`
	LastSeq             int64          `json:"last_seq"`
	HeartbeatIntervalMS int            `json:"heartbeat_interval_ms"`
	HeartbeatTimeoutMS  int            `json:"heartbeat_timeout_ms"`
	ReconnectWindowMS   int            `json:"reconnect_window_ms"`
}

type memberJoinedPayload struct {
	Member memberObject `json:"member"`
}

type memberLeftPayload struct {
	MemberID string    `json:"member_id"`
	LeftAt   time.Time `json:"left_at"`
}

type memberObject struct {
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

type heartbeatPingPayload struct {
	PingID     string    `json:"ping_id"`
	ServerTime time.Time `json:"server_time"`
}

type roomErrorPayload struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	RequestID *string `json:"request_id"`
	Retryable bool    `json:"retryable"`
}
