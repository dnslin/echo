package ws

import (
	"context"
	"net/http"
	"testing"
	"time"

	"echo/services/api/internal/domain"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestMemberMuteChangedBroadcastsAndSnapshotReflectsMutedState(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	joined := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, joined.Room.ID, joined.RoomSessionToken)
	defer bobConn.Close(websocket.StatusNormalClosure, "test done")
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")

	writeClientCommand(t, bobConn, map[string]any{"type": "member.mute_changed", "payload": map[string]any{"muted": true}})

	for name, conn := range map[string]*websocket.Conn{"host": hostConn, "bob": bobConn} {
		event := readEvent(t, conn)
		assertEventType(t, event, "member.muted_changed")
		var payload memberMutedChangedPayload
		decodePayload(t, event.Payload, &payload)
		if payload.MemberID != joined.Member.ID || !payload.Muted || payload.ChangedAt.IsZero() {
			t.Fatalf("%s muted payload = %#v, want Bob muted with changed_at", name, payload)
		}
	}

	member, err := integration.repository.FindMemberByRoomAndID(context.Background(), created.Room.ID, joined.Member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if !member.Muted {
		t.Fatalf("repository muted = %v, want true", member.Muted)
	}

	writeClientCommand(t, hostConn, map[string]any{"type": "room.resync_requested", "payload": map[string]any{"reason": "after_mute"}})
	resync := readEvent(t, hostConn)
	assertEventType(t, resync, "room.snapshot")
	var snapshot snapshotPayload
	decodePayload(t, resync.Payload, &snapshot)
	bobMember := findSnapshotMember(t, snapshot, joined.Member.ID)
	if !bobMember.Muted {
		t.Fatalf("snapshot muted = %v, want true", bobMember.Muted)
	}
}

func TestMemberSpeakingChangedBroadcastsWithoutReorderingMembers(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	joined := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	third := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Carol", "avatar_09")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, joined.Room.ID, joined.RoomSessionToken)
	defer bobConn.Close(websocket.StatusNormalClosure, "test done")
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")

	writeClientCommand(t, bobConn, map[string]any{"type": "member.speaking_changed", "payload": map[string]any{"speaking": true}})

	for name, conn := range map[string]*websocket.Conn{"host": hostConn, "bob": bobConn} {
		event := readEvent(t, conn)
		assertEventType(t, event, "member.speaking_changed")
		var payload memberSpeakingChangedPayload
		decodePayload(t, event.Payload, &payload)
		if payload.MemberID != joined.Member.ID || !payload.Speaking || payload.ChangedAt.IsZero() {
			t.Fatalf("%s speaking payload = %#v, want Bob speaking with changed_at", name, payload)
		}
	}

	writeClientCommand(t, hostConn, map[string]any{"type": "room.resync_requested", "payload": map[string]any{"reason": "after_speaking"}})
	resync := readEvent(t, hostConn)
	assertEventType(t, resync, "room.snapshot")
	var snapshot snapshotPayload
	decodePayload(t, resync.Payload, &snapshot)
	wantOrder := []string{created.Member.ID, joined.Member.ID, third.Member.ID}
	if len(snapshot.Members) != len(wantOrder) {
		t.Fatalf("snapshot member count = %d, want %d", len(snapshot.Members), len(wantOrder))
	}
	for i, wantID := range wantOrder {
		if snapshot.Members[i].MemberID != wantID {
			t.Fatalf("snapshot member[%d].member_id = %q, want %q", i, snapshot.Members[i].MemberID, wantID)
		}
	}
	if !findSnapshotMember(t, snapshot, joined.Member.ID).Speaking {
		t.Fatalf("Bob speaking in snapshot = false, want true")
	}
}

func TestUnexpectedDisconnectBroadcastsReconnectingAndClearsSpeaking(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	joined := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, joined.Room.ID, joined.RoomSessionToken)
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")

	writeClientCommand(t, bobConn, map[string]any{"type": "member.speaking_changed", "payload": map[string]any{"speaking": true}})
	assertEventType(t, readEvent(t, hostConn), "member.speaking_changed")
	assertEventType(t, readEvent(t, bobConn), "member.speaking_changed")
	if err := bobConn.Close(websocket.StatusNormalClosure, "simulate drop"); err != nil {
		t.Fatalf("bobConn.Close returned error: %v", err)
	}

	speakingCleared := readEvent(t, hostConn)
	assertEventType(t, speakingCleared, "member.speaking_changed")
	var speakingPayload memberSpeakingChangedPayload
	decodePayload(t, speakingCleared.Payload, &speakingPayload)
	if speakingPayload.MemberID != joined.Member.ID || speakingPayload.Speaking {
		t.Fatalf("speaking cleared payload = %#v, want Bob false", speakingPayload)
	}
	reconnecting := readEvent(t, hostConn)
	assertEventType(t, reconnecting, "member.reconnecting")
	var reconnectingPayload memberReconnectingPayload
	decodePayload(t, reconnecting.Payload, &reconnectingPayload)
	if reconnectingPayload.MemberID != joined.Member.ID || reconnectingPayload.ReconnectWindowMS != 30000 || reconnectingPayload.ReconnectUntil.IsZero() {
		t.Fatalf("reconnecting payload = %#v, want Bob deadline and 30000ms", reconnectingPayload)
	}

	writeClientCommand(t, hostConn, map[string]any{"type": "room.resync_requested", "payload": map[string]any{"reason": "after_disconnect"}})
	resync := readEvent(t, hostConn)
	assertEventType(t, resync, "room.snapshot")
	var snapshot snapshotPayload
	decodePayload(t, resync.Payload, &snapshot)
	bobMember := findSnapshotMember(t, snapshot, joined.Member.ID)
	if bobMember.State != string(domain.MemberStateReconnecting) || bobMember.Speaking || bobMember.ReconnectUntil == nil {
		t.Fatalf("snapshot Bob projection = %#v, want reconnecting false speaking with reconnect_until", bobMember)
	}
	member, err := integration.repository.FindMemberByRoomAndID(context.Background(), created.Room.ID, joined.Member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if member.State != domain.MemberStateOnline {
		t.Fatalf("repository state after unexpected disconnect = %q, want online during reconnect window", member.State)
	}
}

func TestReconnectWithinWindowRestoresOriginalMemberIdentity(t *testing.T) {
	integration := newWSIntegration(t, func(config *Config) {
		config.ReconnectWindow = 200 * time.Millisecond
	})
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	joined := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, joined.Room.ID, joined.RoomSessionToken)
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")
	if err := bobConn.Close(websocket.StatusNormalClosure, "simulate drop"); err != nil {
		t.Fatalf("bobConn.Close returned error: %v", err)
	}
	assertEventType(t, readEvent(t, hostConn), "member.reconnecting")

	restoredConn := dialRoomWebSocket(t, integration.server.URL, joined.Room.ID, joined.RoomSessionToken)
	defer restoredConn.Close(websocket.StatusNormalClosure, "test done")
	restoredSnapshot := readEvent(t, restoredConn)
	assertEventType(t, restoredSnapshot, "room.snapshot")
	var snapshot snapshotPayload
	decodePayload(t, restoredSnapshot.Payload, &snapshot)
	if snapshot.SelfMemberID != joined.Member.ID {
		t.Fatalf("restored snapshot self_member_id = %q, want %q", snapshot.SelfMemberID, joined.Member.ID)
	}
	if findSnapshotMember(t, snapshot, joined.Member.ID).State != string(domain.MemberStateOnline) {
		t.Fatalf("restored snapshot member state = %q, want online", findSnapshotMember(t, snapshot, joined.Member.ID).State)
	}

	hostRestored := readEvent(t, hostConn)
	assertEventType(t, hostRestored, "member.restored")
	var restoredPayload memberRestoredPayload
	decodePayload(t, hostRestored.Payload, &restoredPayload)
	if restoredPayload.Member.MemberID != joined.Member.ID || restoredPayload.Member.State != string(domain.MemberStateOnline) || restoredPayload.RestoredAt.IsZero() {
		t.Fatalf("host restored payload = %#v, want Bob online with restored_at", restoredPayload)
	}
	selfRestored := readEvent(t, restoredConn)
	assertEventType(t, selfRestored, "member.restored")
}

func TestReconnectTimeoutBroadcastsDisconnectedAndFreesCapacity(t *testing.T) {
	integration := newWSIntegration(t, func(config *Config) {
		config.ReconnectWindow = 50 * time.Millisecond
	})
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	joinedMembers := make([]roomCredentialResponse, 0, 9)
	for i := range 9 {
		joinedMembers = append(joinedMembers, joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Guest"+string(rune('A'+i)), "avatar_08"))
	}
	target := joinedMembers[len(joinedMembers)-1]
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	targetConn := dialRoomWebSocket(t, integration.server.URL, target.Room.ID, target.RoomSessionToken)
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, targetConn), "room.snapshot")
	if err := targetConn.Close(websocket.StatusNormalClosure, "simulate drop"); err != nil {
		t.Fatalf("targetConn.Close returned error: %v", err)
	}
	assertEventType(t, readEvent(t, hostConn), "member.reconnecting")

	roomFull := performWSJSONRequest(t, integration.router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  created.Room.InviteCode,
		"anonymous_id": "anon_over_capacity",
		"nickname":     "LaterJoiner",
		"avatar_id":    "avatar_09",
	})
	assertWSHTTPError(t, roomFull, http.StatusConflict, "room_full", "房间人数已满，暂时无法加入")

	disconnected := readEvent(t, hostConn)
	assertEventType(t, disconnected, "member.disconnected")
	var disconnectedPayload memberDisconnectedPayload
	decodePayload(t, disconnected.Payload, &disconnectedPayload)
	if disconnectedPayload.MemberID != target.Member.ID || disconnectedPayload.Reason != "reconnect_timeout" || disconnectedPayload.DisconnectedAt.IsZero() {
		t.Fatalf("member.disconnected payload = %#v, want timed-out member with reason", disconnectedPayload)
	}
	member, err := integration.repository.FindMemberByRoomAndID(context.Background(), created.Room.ID, target.Member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if member.State != domain.MemberStateDisconnected {
		t.Fatalf("repository member state after timeout = %q, want disconnected", member.State)
	}

	joinedAfterTimeout := performWSJSONRequest(t, integration.router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  created.Room.InviteCode,
		"anonymous_id": "anon_after_timeout",
		"nickname":     "AfterTimeout",
		"avatar_id":    "avatar_09",
	})
	if joinedAfterTimeout.Code != http.StatusOK {
		t.Fatalf("POST /v1/rooms/join after timeout status = %d, want 200, body: %s", joinedAfterTimeout.Code, joinedAfterTimeout.Body.String())
	}
}

func TestHTTPLeaveDoesNotTriggerReconnectFlow(t *testing.T) {
	integration := newWSIntegration(t)
	created := createRoomThroughHTTP(t, integration.router, "Alice", "avatar_07")
	joined := joinRoomThroughHTTP(t, integration.router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, integration.server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, integration.server.URL, joined.Room.ID, joined.RoomSessionToken)
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")

	leaveResponse := performAuthorizedWSJSONRequest(t, integration.router, http.MethodPost, "/v1/rooms/"+created.Room.ID+"/leave", joined.RoomSessionToken, map[string]string{"member_id": joined.Member.ID})
	if leaveResponse.Code != http.StatusNoContent {
		t.Fatalf("leave status = %d, want 204, body: %s", leaveResponse.Code, leaveResponse.Body.String())
	}
	assertEventType(t, readEvent(t, hostConn), "member.left")
	assertEventType(t, readEvent(t, bobConn), "member.left")
	assertConnectionCloses(t, bobConn)
	assertNoEventWithin(t, hostConn, 150*time.Millisecond)
}

func findSnapshotMember(t *testing.T, snapshot snapshotPayload, memberID string) memberObject {
	t.Helper()
	for _, member := range snapshot.Members {
		if member.MemberID == memberID {
			return member
		}
	}
	t.Fatalf("snapshot missing member %q: %#v", memberID, snapshot.Members)
	return memberObject{}
}

func assertNoEventWithin(t *testing.T, conn *websocket.Conn, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var event testEvent
	if err := wsjson.Read(ctx, conn, &event); err == nil {
		t.Fatalf("wsjson.Read returned event %#v, want no event within %v", event, timeout)
	}
}

type memberMutedChangedPayload struct {
	MemberID  string    `json:"member_id"`
	Muted     bool      `json:"muted"`
	ChangedAt time.Time `json:"changed_at"`
}

type memberSpeakingChangedPayload struct {
	MemberID  string    `json:"member_id"`
	Speaking  bool      `json:"speaking"`
	ChangedAt time.Time `json:"changed_at"`
}

type memberReconnectingPayload struct {
	MemberID          string    `json:"member_id"`
	ReconnectUntil    time.Time `json:"reconnect_until"`
	ReconnectWindowMS int       `json:"reconnect_window_ms"`
}

type memberRestoredPayload struct {
	Member     memberObject `json:"member"`
	RestoredAt time.Time    `json:"restored_at"`
}

type memberDisconnectedPayload struct {
	MemberID       string    `json:"member_id"`
	DisconnectedAt time.Time `json:"disconnected_at"`
	Reason         string    `json:"reason"`
}
