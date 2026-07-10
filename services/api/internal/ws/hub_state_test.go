package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"echo/services/api/internal/domain"
	httpapi "echo/services/api/internal/http"
	"echo/services/api/internal/room"
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

func TestMuteSpeakingClearUsesOneOutboundGroup(t *testing.T) {
	roomValue := wsTestRoom("room_mute_group", "MUTGRP", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_mute_group", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	member.Speaking = true
	mutator := stateMutatorFuncs{
		updateMute: func(context.Context, room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error) {
			member.Muted = true
			member.Speaking = false
			return room.UpdateMemberMuteResult{Member: member, MutedChanged: true, SpeakingChanged: true}, nil
		},
	}
	hub := NewHub(Config{
		Authorizer:          fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:       fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:        mutator,
		RoomSessionSecret:   wsTestSessionSecret,
		Now:                 func() time.Time { return wsTestNow },
		WriteTimeout:        time.Second,
		ConnectionQueueSize: 1,
	})
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.byMember[member.ID] = client
	roomState.lastSpeakingAccepted[member.ID] = wsTestNow
	hub.mu.Unlock()

	client.handleMuteChanged(json.RawMessage(`{"muted":true}`), nil)

	if err := client.ctx.Err(); err != nil {
		t.Fatalf("queue-size-one mute connection was closed: %v", err)
	}
	outbound := <-client.send
	if len(outbound.events) != 2 {
		t.Fatalf("mute group event count = %d, want 2", len(outbound.events))
	}
	if outbound.events[0].Type != "member.speaking_changed" || outbound.events[1].Type != "member.muted_changed" {
		t.Fatalf("mute group types = %q, %q", outbound.events[0].Type, outbound.events[1].Type)
	}
	if outbound.events[1].Seq != outbound.events[0].Seq+1 {
		t.Fatalf("mute group seq = %d, %d, want consecutive", outbound.events[0].Seq, outbound.events[1].Seq)
	}
	select {
	case extra := <-client.send:
		t.Fatalf("mute transition used an extra queue slot: %#v", extra.events)
	default:
	}
	hub.mu.Lock()
	_, throttled := roomState.lastSpeakingAccepted[member.ID]
	hub.mu.Unlock()
	if throttled {
		t.Fatalf("server-forced mute clear retained speaking throttle timestamp")
	}
}

func TestServerForcedSpeakingClearDoesNotThrottleNextClientTrue(t *testing.T) {
	clock := newControlledClock(wsTestNow)
	roomValue := wsTestRoom("room_forced_clear_throttle", "THROT1", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_forced_clear_throttle", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	member.Speaking = true
	speakingCalls := 0
	mutator := stateMutatorFuncs{
		updateMute: func(_ context.Context, input room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error) {
			mutedChanged := member.Muted != input.Muted
			speakingChanged := input.Muted && member.Speaking
			member.Muted = input.Muted
			if input.Muted {
				member.Speaking = false
			}
			return room.UpdateMemberMuteResult{Member: member, MutedChanged: mutedChanged, SpeakingChanged: speakingChanged}, nil
		},
		updateSpeaking: func(_ context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
			speakingCalls++
			changed := member.Speaking != input.Speaking
			member.Speaking = input.Speaking
			return room.UpdateMemberSpeakingResult{Member: member, Changed: changed}, nil
		},
	}
	hub := NewHub(Config{
		Authorizer:          fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:       fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:        mutator,
		RoomSessionSecret:   wsTestSessionSecret,
		Now:                 clock.Now,
		WriteTimeout:        time.Second,
		SpeakingMinInterval: time.Second,
	})
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.byMember[member.ID] = client
	hub.mu.Unlock()

	client.handleMuteChanged(json.RawMessage(`{"muted":true}`), nil)
	<-client.send
	client.handleMuteChanged(json.RawMessage(`{"muted":false}`), nil)
	<-client.send
	clock.Set(wsTestNow.Add(time.Millisecond))
	client.handleSpeakingChanged(json.RawMessage(`{"speaking":true}`), nil)

	if speakingCalls != 1 {
		t.Fatalf("client speaking mutation calls = %d, want 1", speakingCalls)
	}
	outbound := <-client.send
	if len(outbound.events) != 1 || outbound.events[0].Type != "member.speaking_changed" {
		t.Fatalf("client speaking group = %#v, want speaking_changed", outbound.events)
	}
	hub.mu.Lock()
	acceptedAt := roomState.lastSpeakingAccepted[member.ID]
	hub.mu.Unlock()
	if !acceptedAt.Equal(wsTestNow.Add(time.Millisecond)) {
		t.Fatalf("last accepted speaking time = %v, want client report time", acceptedAt)
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
	if restoredPayload.Member.MemberID != joined.Member.ID || restoredPayload.Member.State != string(domain.MemberStateOnline) || restoredPayload.Member.Speaking || restoredPayload.Member.IsSelf || restoredPayload.RestoredAt.IsZero() {
		t.Fatalf("host restored payload = %#v, want Bob online, not speaking, and not self", restoredPayload)
	}
	selfRestored := readEvent(t, restoredConn)
	assertEventType(t, selfRestored, "member.restored")
	var selfRestoredPayload memberRestoredPayload
	decodePayload(t, selfRestored.Payload, &selfRestoredPayload)
	if selfRestoredPayload.Member.MemberID != joined.Member.ID || !selfRestoredPayload.Member.IsSelf || selfRestoredPayload.Member.Speaking {
		t.Fatalf("self restored payload = %#v, want Bob as self and not speaking", selfRestoredPayload)
	}
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

func TestReconnectRegistrationRejectsExpiredDeadlineAtLinearizationPoint(t *testing.T) {
	now := wsTestNow
	roomValue := wsTestRoom("room_expired_restore", "RESTEX", domain.RoomStateActive, now)
	member := wsTestMember("mem_expired_restore", roomValue.ID, domain.MemberStateOnline, false, now)
	hub := NewHub(Config{
		Authorizer: authorizerFunc(func(context.Context, room.AuthorizeMemberInput) (room.AuthorizeMemberResult, error) {
			now = now.Add(2 * time.Second)
			return room.AuthorizeMemberResult{Room: roomValue, Member: member}, nil
		}),
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      fixedStateMutator{},
		RoomSessionSecret: wsTestSessionSecret,
		Now:               func() time.Time { return now },
		ReconnectWindow:   time.Second,
		WriteTimeout:      time.Second,
	})
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.reconnecting[member.ID] = reconnectingMember{deadline: now.Add(time.Second), generation: 1, phase: reconnectPhaseRestorable}
	hub.mu.Unlock()
	client := hub.newConnection(nil, roomValue.ID, member.ID)

	err := hub.registerWithInitialSnapshot(client)
	if err == nil {
		t.Fatalf("registerWithInitialSnapshot succeeded after reconnect deadline")
	}
	hub.mu.Lock()
	registered := roomState.byMember[member.ID] == client
	hub.mu.Unlock()
	if registered {
		t.Fatalf("expired reconnect registered the member connection")
	}
	select {
	case outbound := <-client.send:
		t.Fatalf("expired reconnect queued event %q", outbound.events[0].Type)
	default:
	}
}

func TestReconnectRegistrationRejectsNonRestorableRecord(t *testing.T) {
	roomValue := wsTestRoom("room_pending_restore", "PENDNG", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_pending_restore", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      fixedStateMutator{},
		RoomSessionSecret: wsTestSessionSecret,
		Now:               func() time.Time { return wsTestNow },
		ReconnectWindow:   time.Second,
		WriteTimeout:      time.Second,
	})
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.reconnecting[member.ID] = reconnectingMember{deadline: wsTestNow.Add(time.Second), generation: 1}
	hub.mu.Unlock()
	client := hub.newConnection(nil, roomValue.ID, member.ID)

	if err := hub.registerWithInitialSnapshot(client); err == nil {
		t.Fatalf("registerWithInitialSnapshot succeeded for non-restorable reconnect record")
	}
	hub.mu.Lock()
	registered := roomState.byMember[member.ID] == client
	hub.mu.Unlock()
	if registered {
		t.Fatalf("non-restorable reconnect registered the member connection")
	}
	select {
	case outbound := <-client.send:
		t.Fatalf("non-restorable reconnect queued event %q", outbound.events[0].Type)
	default:
	}
}

func TestRegistrationReauthorizesMemberAtLinearizationPoint(t *testing.T) {
	roomValue := wsTestRoom("room_fresh_authorization", "FRESHA", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_fresh_authorization", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{err: room.ErrMemberNotActive},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      fixedStateMutator{},
		RoomSessionSecret: wsTestSessionSecret,
		Now:               func() time.Time { return wsTestNow },
		WriteTimeout:      time.Second,
	})
	client := hub.newConnection(nil, roomValue.ID, member.ID)

	err := hub.registerWithInitialSnapshot(client)
	if !errors.Is(err, room.ErrMemberNotActive) {
		t.Fatalf("registerWithInitialSnapshot error = %v, want member not active", err)
	}
	select {
	case outbound := <-client.send:
		t.Fatalf("inactive member queued event %q", outbound.events[0].Type)
	default:
	}
	if hubHasRoomState(hub, roomValue.ID) {
		t.Fatalf("failed registration retained empty room state")
	}
}

func TestSnapshotFailurePrunesEmptyRoomState(t *testing.T) {
	roomValue := wsTestRoom("room_snapshot_failure", "SNAPFL", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_snapshot_failure", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:     fixedSnapshotStore{roomErr: errors.New("snapshot unavailable")},
		StateMutator:      fixedStateMutator{},
		RoomSessionSecret: wsTestSessionSecret,
		Now:               func() time.Time { return wsTestNow },
		WriteTimeout:      time.Second,
	})
	client := hub.newConnection(nil, roomValue.ID, member.ID)

	if err := hub.registerWithInitialSnapshot(client); err == nil {
		t.Fatalf("registerWithInitialSnapshot succeeded with snapshot failure")
	}
	if hubHasRoomState(hub, roomValue.ID) {
		t.Fatalf("snapshot failure retained empty room state")
	}
}

func TestMemberLeftClearsSpeakingThrottleKey(t *testing.T) {
	roomValue := wsTestRoom("room_left_throttle_cleanup", "LEFTCL", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_left_throttle_cleanup", roomValue.ID, domain.MemberStateDisconnected, false, wsTestNow)
	hub := NewHub(Config{ConnectionQueueSize: 2})
	observer := hub.newConnection(nil, roomValue.ID, "mem_left_cleanup_observer")
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[observer] = struct{}{}
	roomState.byMember[observer.memberID] = observer
	roomState.lastSpeakingAccepted[member.ID] = wsTestNow
	hub.mu.Unlock()

	hub.NotifyMemberLeft(context.Background(), roomValue, member)

	hub.mu.Lock()
	_, retained := roomState.lastSpeakingAccepted[member.ID]
	hub.mu.Unlock()
	if retained {
		t.Fatalf("member.left retained speaking throttle timestamp")
	}
	outbound := <-observer.send
	if len(outbound.events) != 1 || outbound.events[0].Type != "member.left" {
		t.Fatalf("member.left outbound group = %#v", outbound.events)
	}
}

func TestReconnectTimeoutClearsSpeakingThrottleKey(t *testing.T) {
	roomValue := wsTestRoom("room_timeout_throttle_cleanup", "TIMECL", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_timeout_throttle_cleanup", roomValue.ID, domain.MemberStateDisconnected, false, wsTestNow)
	mutator := stateMutatorFuncs{
		disconnect: func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error) {
			return room.LeaveResult{Room: roomValue, Member: member, Transitioned: true}, nil
		},
	}
	hub := NewHub(Config{
		StateMutator:        mutator,
		Now:                 func() time.Time { return wsTestNow },
		ConnectionQueueSize: 2,
	})
	observer := hub.newConnection(nil, roomValue.ID, "mem_timeout_cleanup_observer")
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[observer] = struct{}{}
	roomState.byMember[observer.memberID] = observer
	roomState.reconnecting[member.ID] = reconnectingMember{
		deadline:   wsTestNow,
		generation: 1,
		phase:      reconnectPhaseTimingOut,
	}
	roomState.lastSpeakingAccepted[member.ID] = wsTestNow
	hub.mu.Unlock()

	hub.handleReconnectTimeout(roomValue.ID, member.ID, 1)

	hub.mu.Lock()
	_, retained := roomState.lastSpeakingAccepted[member.ID]
	_, reconnecting := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	if retained {
		t.Fatalf("reconnect timeout retained speaking throttle timestamp")
	}
	if reconnecting {
		t.Fatalf("reconnect timeout retained reconnect overlay")
	}
	outbound := <-observer.send
	if len(outbound.events) != 1 || outbound.events[0].Type != "member.disconnected" {
		t.Fatalf("reconnect timeout outbound group = %#v", outbound.events)
	}
}

func TestInFlightCommandFromDetachedConnectionDoesNotBroadcast(t *testing.T) {
	roomValue := wsTestRoom("room_stale_command", "STALE1", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_stale_command", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	mutator := newBlockingSpeakingStateMutator(member)
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      mutator,
		RoomSessionSecret: wsTestSessionSecret,
		Now:               func() time.Time { return wsTestNow },
		ReconnectWindow:   time.Hour,
		WriteTimeout:      time.Second,
	})
	oldConnection := hub.newConnection(nil, roomValue.ID, member.ID)
	observer := hub.newConnection(nil, roomValue.ID, "mem_observer")
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[oldConnection] = struct{}{}
	roomState.connections[observer] = struct{}{}
	roomState.byMember[member.ID] = oldConnection
	roomState.byMember[observer.memberID] = observer
	hub.mu.Unlock()

	handled := make(chan struct{})
	go func() {
		oldConnection.handleSpeakingChanged(json.RawMessage(`{"speaking":true}`), nil)
		close(handled)
	}()
	<-mutator.entered

	released := false
	defer func() {
		if !released {
			close(mutator.release)
		}
	}()
	oldConnection.cancel()
	disconnected := make(chan struct{})
	go func() {
		hub.handleUnexpectedDisconnect(oldConnection)
		close(disconnected)
	}()
	waitForHubRoomRefs(t, hub, roomValue.ID, 2)
	select {
	case <-disconnected:
		t.Fatalf("disconnect completed while the same-room command still owned the transition gate")
	default:
	}

	close(mutator.release)
	released = true
	<-handled
	<-disconnected

	reconnecting := <-observer.send
	if reconnecting.events[0].Type != "member.reconnecting" {
		t.Fatalf("event after detached command = %q, want member.reconnecting", reconnecting.events[0].Type)
	}
	select {
	case outbound := <-observer.send:
		t.Fatalf("detached connection broadcast event %q", outbound.events[0].Type)
	default:
	}

	hub.mu.Lock()
	if reconnectingMember := roomState.reconnecting[member.ID]; reconnectingMember.timer != nil {
		reconnectingMember.timer.Stop()
	}
	hub.mu.Unlock()
}

func TestBlockedStateMutationForOneRoomDoesNotBlockAnotherRoom(t *testing.T) {
	roomA := wsTestRoom("room_blocked_mutation_a", "MUTATA", domain.RoomStateActive, wsTestNow)
	memberA := wsTestMember("mem_blocked_mutation_a", roomA.ID, domain.MemberStateOnline, false, wsTestNow)
	roomB := wsTestRoom("room_mutation_progress_b", "MUTATB", domain.RoomStateActive, wsTestNow)
	memberB := wsTestMember("mem_mutation_progress_b", roomB.ID, domain.MemberStateOnline, true, wsTestNow)
	joinedB := wsTestMember("mem_mutation_joined_b", roomB.ID, domain.MemberStateOnline, false, wsTestNow.Add(time.Minute))
	mutator := newBlockingSpeakingStateMutator(memberA)
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomA, Member: memberA}},
		SnapshotStore:     fixedSnapshotStore{room: roomA, members: []domain.Member{memberA}},
		StateMutator:      mutator,
		RoomSessionSecret: wsTestSessionSecret,
		Now:               func() time.Time { return wsTestNow },
		WriteTimeout:      time.Second,
	})
	clientA := hub.newConnection(nil, roomA.ID, memberA.ID)
	observerB := hub.newConnection(nil, roomB.ID, memberB.ID)
	hub.mu.Lock()
	roomStateA := hub.roomStateLocked(roomA.ID)
	roomStateA.connections[clientA] = struct{}{}
	roomStateA.byMember[memberA.ID] = clientA
	roomStateB := hub.roomStateLocked(roomB.ID)
	roomStateB.connections[observerB] = struct{}{}
	roomStateB.byMember[memberB.ID] = observerB
	hub.mu.Unlock()

	handled := make(chan struct{})
	go func() {
		clientA.handleSpeakingChanged(json.RawMessage(`{"speaking":true}`), nil)
		close(handled)
	}()
	<-mutator.entered

	joinedDone := make(chan struct{})
	go func() {
		hub.NotifyMemberJoined(context.Background(), roomB, joinedB)
		close(joinedDone)
	}()
	progressed := false
	select {
	case <-joinedDone:
		progressed = true
	case <-time.After(100 * time.Millisecond):
	}
	close(mutator.release)
	<-handled
	<-joinedDone
	if !progressed {
		t.Fatalf("room B join notification waited for room A state mutation")
	}
	if outbound := <-observerB.send; outbound.events[0].Type != "member.joined" {
		t.Fatalf("room B event type = %q, want member.joined", outbound.events[0].Type)
	}
}

func TestReconnectSpeakingClearFailureStaysPendingWithoutBroadcast(t *testing.T) {
	clock := newControlledClock(wsTestNow)
	scheduler := newManualReconnectScheduler()
	roomValue := wsTestRoom("room_clear_pending", "CLEAR1", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_clear_pending", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	member.Speaking = true
	clearCalls := 0
	mutator := stateMutatorFuncs{
		updateSpeaking: func(_ context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
			clearCalls++
			if clearCalls == 1 {
				return room.UpdateMemberSpeakingResult{}, errors.New("temporary speaking store failure")
			}
			member.Speaking = input.Speaking
			return room.UpdateMemberSpeakingResult{Member: member, Changed: true}, nil
		},
	}
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      mutator,
		RoomSessionSecret: wsTestSessionSecret,
		Now:               clock.Now,
		ReconnectWindow:   time.Hour,
		WriteTimeout:      time.Second,
	})
	hub.afterFunc = scheduler.AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	observer := hub.newConnection(nil, roomValue.ID, "mem_clear_observer")
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.connections[observer] = struct{}{}
	roomState.byMember[member.ID] = client
	roomState.byMember[observer.memberID] = observer
	hub.mu.Unlock()

	hub.handleUnexpectedDisconnect(client)

	select {
	case outbound := <-observer.send:
		t.Fatalf("temporary speaking clear failure broadcast %q", outbound.events[0].Type)
	default:
	}
	hub.mu.Lock()
	reconnecting, ok := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	if !ok || reconnecting.phase != reconnectPhasePending {
		t.Fatalf("temporary speaking clear failure overlay = %#v, want pending", reconnecting)
	}
	wantDeadline := wsTestNow.Add(time.Hour)
	if !reconnecting.deadline.Equal(wantDeadline) {
		t.Fatalf("pending deadline = %v, want %v", reconnecting.deadline, wantDeadline)
	}

	clock.Set(wsTestNow.Add(reconnectRetryDelay))
	if delay := scheduler.RunNext(t); delay != reconnectRetryDelay {
		t.Fatalf("speaking clear retry delay = %v, want %v", delay, reconnectRetryDelay)
	}
	if clearCalls != 2 {
		t.Fatalf("speaking clear calls = %d, want 2", clearCalls)
	}
	reconnectEvents := <-observer.send
	if len(reconnectEvents.events) != 2 {
		t.Fatalf("durable reconnect group event count = %d, want 2", len(reconnectEvents.events))
	}
	if reconnectEvents.events[0].Type != "member.speaking_changed" || reconnectEvents.events[1].Type != "member.reconnecting" {
		t.Fatalf("durable reconnect group types = %q, %q", reconnectEvents.events[0].Type, reconnectEvents.events[1].Type)
	}
	if reconnectEvents.events[1].Seq != reconnectEvents.events[0].Seq+1 {
		t.Fatalf("durable reconnect group seq = %d, %d, want consecutive", reconnectEvents.events[0].Seq, reconnectEvents.events[1].Seq)
	}
	payload := reconnectEvents.events[1].Payload.(reconnectingMessagePayload)
	if !payload.ReconnectUntil.Equal(wantDeadline) {
		t.Fatalf("retry reconnect_until = %v, want original deadline %v", payload.ReconnectUntil, wantDeadline)
	}
	hub.mu.Lock()
	reconnecting = roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	if reconnecting.phase != reconnectPhaseRestorable {
		t.Fatalf("reconnect phase after durable clear = %d, want restorable", reconnecting.phase)
	}
	if reconnecting.timer != nil {
		reconnecting.timer.Stop()
	}
}

func TestReconnectDeadlineUsesTransportLossTimeBeforeRoomGateWait(t *testing.T) {
	clock := newControlledClock(wsTestNow)
	scheduler := newManualReconnectScheduler()
	roomValue := wsTestRoom("room_original_deadline", "ORIGDL", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_original_deadline", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      fixedStateMutator{},
		RoomSessionSecret: wsTestSessionSecret,
		Now:               clock.Now,
		ReconnectWindow:   time.Minute,
		WriteTimeout:      time.Second,
	})
	hub.afterFunc = scheduler.AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.byMember[member.ID] = client
	hub.mu.Unlock()

	roomState.transition.Lock()
	done := make(chan struct{})
	go func() {
		hub.handleUnexpectedDisconnect(client)
		close(done)
	}()
	waitForHubRoomRefs(t, hub, roomValue.ID, 1)
	clock.Set(wsTestNow.Add(30 * time.Second))
	roomState.transition.Unlock()
	<-done

	hub.mu.Lock()
	reconnecting := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	wantDeadline := wsTestNow.Add(time.Minute)
	if !reconnecting.deadline.Equal(wantDeadline) {
		t.Fatalf("reconnect deadline = %v, want loss-time deadline %v", reconnecting.deadline, wantDeadline)
	}
	if reconnecting.timer != nil {
		reconnecting.timer.Stop()
	}
}

func TestReconnectTimeoutTransitionedFalseDoesNotBroadcastTerminalEvent(t *testing.T) {
	clock := newControlledClock(wsTestNow)
	scheduler := newManualReconnectScheduler()
	roomValue := wsTestRoom("room_terminal_loser", "LOSER1", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_terminal_loser", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	mutator := stateMutatorFuncs{
		disconnect: func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error) {
			member.State = domain.MemberStateDisconnected
			return room.LeaveResult{Room: roomValue, Member: member, Transitioned: false}, nil
		},
	}
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      mutator,
		RoomSessionSecret: wsTestSessionSecret,
		Now:               clock.Now,
		ReconnectWindow:   time.Minute,
		WriteTimeout:      time.Second,
	})
	hub.afterFunc = scheduler.AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	observer := hub.newConnection(nil, roomValue.ID, "mem_terminal_observer")
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.connections[observer] = struct{}{}
	roomState.byMember[member.ID] = client
	roomState.byMember[observer.memberID] = observer
	hub.mu.Unlock()

	hub.handleUnexpectedDisconnect(client)
	if outbound := <-observer.send; outbound.events[0].Type != "member.reconnecting" {
		t.Fatalf("initial event type = %q, want member.reconnecting", outbound.events[0].Type)
	}
	hub.mu.Lock()
	reconnecting := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	clock.Set(reconnecting.deadline)
	if delay := scheduler.RunNext(t); delay != time.Minute {
		t.Fatalf("deadline timer delay = %v, want %v", delay, time.Minute)
	}

	select {
	case outbound := <-observer.send:
		t.Fatalf("Transitioned=false broadcast terminal event %q", outbound.events[0].Type)
	default:
	}
	hub.mu.Lock()
	_, retained := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	if retained {
		t.Fatalf("Transitioned=false retained reconnect overlay")
	}
}

func TestReconnectTimeoutRetriesBeyondThreeFailures(t *testing.T) {
	clock := newControlledClock(wsTestNow)
	scheduler := newManualReconnectScheduler()
	roomValue := wsTestRoom("room_retry_terminal", "RETRY1", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_retry_terminal", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	disconnectCalls := 0
	mutator := stateMutatorFuncs{
		disconnect: func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error) {
			disconnectCalls++
			if disconnectCalls <= 3 {
				return room.LeaveResult{}, errors.New("temporary disconnect store failure")
			}
			member.State = domain.MemberStateDisconnected
			return room.LeaveResult{Room: roomValue, Member: member, Transitioned: true}, nil
		},
	}
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      mutator,
		RoomSessionSecret: wsTestSessionSecret,
		Now:               clock.Now,
		ReconnectWindow:   time.Minute,
		WriteTimeout:      time.Second,
	})
	hub.afterFunc = scheduler.AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	observer := hub.newConnection(nil, roomValue.ID, "mem_retry_observer")
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.connections[observer] = struct{}{}
	roomState.byMember[member.ID] = client
	roomState.byMember[observer.memberID] = observer
	hub.mu.Unlock()

	hub.handleUnexpectedDisconnect(client)
	<-observer.send
	hub.mu.Lock()
	reconnecting := roomState.reconnecting[member.ID]
	hub.mu.Unlock()

	for attempt := 1; attempt <= 4; attempt++ {
		clock.Set(reconnecting.deadline.Add(time.Duration(attempt-1) * reconnectRetryDelay))
		delay := scheduler.RunNext(t)
		wantDelay := reconnectRetryDelay
		if attempt == 1 {
			wantDelay = time.Minute
		}
		if delay != wantDelay {
			t.Fatalf("attempt %d timer delay = %v, want %v", attempt, delay, wantDelay)
		}
		hub.mu.Lock()
		current, retained := roomState.reconnecting[member.ID]
		hub.mu.Unlock()
		if attempt <= 3 {
			if !retained || current.phase != reconnectPhaseTimingOut {
				t.Fatalf("reconnect overlay after transient attempt %d = %#v, retained=%v", attempt, current, retained)
			}
			select {
			case outbound := <-observer.send:
				t.Fatalf("transient attempt %d broadcast %q", attempt, outbound.events[0].Type)
			default:
			}
		}
	}
	if disconnectCalls != 4 {
		t.Fatalf("DisconnectMemberContext calls = %d, want 4", disconnectCalls)
	}
	if outbound := <-observer.send; outbound.events[0].Type != "member.disconnected" {
		t.Fatalf("terminal event type = %q, want member.disconnected", outbound.events[0].Type)
	}
}

func TestTimingOutReconnectRemainsProjectedWhileDisconnectRetries(t *testing.T) {
	clock := newControlledClock(wsTestNow)
	scheduler := newManualReconnectScheduler()
	roomValue := wsTestRoom("room_timing_out_snapshot", "TIMOUT", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_timing_out_snapshot", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	mutator := stateMutatorFuncs{
		updateSpeaking: func(context.Context, room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
			return room.UpdateMemberSpeakingResult{Member: member}, nil
		},
		disconnect: func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error) {
			return room.LeaveResult{}, errors.New("temporary disconnect store failure")
		},
	}
	hub := NewHub(Config{
		SnapshotStore:       fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:        mutator,
		Now:                 clock.Now,
		ReconnectWindow:     time.Minute,
		ConnectionQueueSize: 2,
	})
	hub.afterFunc = scheduler.AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	observer := hub.newConnection(nil, roomValue.ID, "mem_timing_out_observer")
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.connections[observer] = struct{}{}
	roomState.byMember[member.ID] = client
	roomState.byMember[observer.memberID] = observer
	hub.mu.Unlock()

	hub.handleUnexpectedDisconnect(client)
	assertOutboundTypes(t, <-observer.send, "member.reconnecting")
	hub.mu.Lock()
	reconnecting := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	clock.Set(reconnecting.deadline)
	if delay := scheduler.RunNext(t); delay != time.Minute {
		t.Fatalf("timeout timer delay = %v, want %v", delay, time.Minute)
	}

	observer.sendSnapshot()
	snapshotGroup := <-observer.send
	assertOutboundTypes(t, snapshotGroup, "room.snapshot")
	snapshot := snapshotGroup.events[0].Payload.(snapshotMessagePayload)
	var projected memberProjection
	for _, candidate := range snapshot.Members {
		if candidate.MemberID == member.ID {
			projected = candidate
			break
		}
	}
	if projected.MemberID == "" {
		t.Fatalf("timing-out snapshot omitted member %q", member.ID)
	}
	if projected.State != string(domain.MemberStateReconnecting) || projected.Speaking || projected.ReconnectUntil == nil || !projected.ReconnectUntil.Equal(reconnecting.deadline) {
		t.Fatalf("timing-out snapshot member = %#v, want reconnecting with original deadline and speaking=false", projected)
	}
	hub.mu.Lock()
	reconnecting = roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	if reconnecting.timer != nil {
		reconnecting.timer.Stop()
	}
}

func TestReconnectBackgroundMutationsUseBoundedContexts(t *testing.T) {
	clock := newControlledClock(wsTestNow)
	scheduler := newManualReconnectScheduler()
	roomValue := wsTestRoom("room_reconnect_context", "CTXBND", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_reconnect_context", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	speakingBounded := false
	disconnectBounded := false
	mutator := stateMutatorFuncs{
		updateSpeaking: func(ctx context.Context, _ room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
			_, speakingBounded = ctx.Deadline()
			return room.UpdateMemberSpeakingResult{Member: member}, nil
		},
		disconnect: func(ctx context.Context, _ room.DisconnectMemberInput) (room.LeaveResult, error) {
			_, disconnectBounded = ctx.Deadline()
			return room.LeaveResult{}, errors.New("temporary disconnect store failure")
		},
	}
	hub := NewHub(Config{
		StateMutator:    mutator,
		Now:             clock.Now,
		ReconnectWindow: time.Minute,
	})
	hub.afterFunc = scheduler.AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.byMember[member.ID] = client
	hub.mu.Unlock()

	hub.handleUnexpectedDisconnect(client)
	if !speakingBounded {
		t.Fatalf("reconnect speaking clear received an unbounded context")
	}
	hub.mu.Lock()
	reconnecting := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	clock.Set(reconnecting.deadline)
	scheduler.RunNext(t)
	if !disconnectBounded {
		t.Fatalf("reconnect timeout disconnect received an unbounded context")
	}
	hub.mu.Lock()
	reconnecting = roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	if reconnecting.timer != nil {
		reconnecting.timer.Stop()
	}
}

func TestPendingReconnectCannotRestoreAtDeadlineWhileDisconnectRetries(t *testing.T) {
	clock := newControlledClock(wsTestNow)
	scheduler := newManualReconnectScheduler()
	roomValue := wsTestRoom("room_pending_deadline", "PENDDL", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_pending_deadline", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	disconnectCalls := 0
	mutator := stateMutatorFuncs{
		updateSpeaking: func(context.Context, room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
			return room.UpdateMemberSpeakingResult{}, errors.New("temporary speaking store failure")
		},
		disconnect: func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error) {
			disconnectCalls++
			return room.LeaveResult{}, errors.New("temporary disconnect store failure")
		},
	}
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:      mutator,
		RoomSessionSecret: wsTestSessionSecret,
		Now:               clock.Now,
		ReconnectWindow:   reconnectRetryDelay,
		WriteTimeout:      time.Second,
	})
	hub.afterFunc = scheduler.AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.byMember[member.ID] = client
	hub.mu.Unlock()

	hub.handleUnexpectedDisconnect(client)
	hub.mu.Lock()
	reconnecting := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	clock.Set(reconnecting.deadline)
	if delay := scheduler.RunNext(t); delay != reconnectRetryDelay {
		t.Fatalf("pending deadline timer delay = %v, want %v", delay, reconnectRetryDelay)
	}
	if disconnectCalls != 1 {
		t.Fatalf("DisconnectMemberContext calls = %d, want 1", disconnectCalls)
	}
	hub.mu.Lock()
	reconnecting = roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	if reconnecting.phase != reconnectPhaseTimingOut {
		t.Fatalf("phase at deadline = %d, want timing out", reconnecting.phase)
	}

	lateClient := hub.newConnection(nil, roomValue.ID, member.ID)
	if err := hub.registerWithInitialSnapshot(lateClient); err == nil {
		t.Fatalf("registerWithInitialSnapshot restored pending member at deadline")
	}
	select {
	case outbound := <-lateClient.send:
		t.Fatalf("late reconnect queued event %q", outbound.events[0].Type)
	default:
	}
	if reconnecting.timer != nil {
		reconnecting.timer.Stop()
	}
}

func TestHTTPLeaveAndReconnectTimeoutEmitSingleTerminalEvent(t *testing.T) {
	integration := newWSIntegration(t)
	clock := newControlledClock(wsTestNow)
	scheduler := newManualReconnectScheduler()
	integration.hub.now = clock.Now
	integration.hub.reconnectWindow = time.Minute
	integration.hub.afterFunc = scheduler.AfterFunc
	blockingDisconnect := newBlockingDisconnectStateMutator(integration.hub.stateMutator)
	integration.hub.stateMutator = blockingDisconnect
	leaver := &signalingRoomLeaver{
		delegate:  integration.roomService,
		committed: make(chan room.LeaveResult, 1),
	}
	router := httpapi.NewRouter(
		httpapi.WithRoomCreator(integration.roomService),
		httpapi.WithRoomJoiner(integration.roomService),
		httpapi.WithRoomLeaver(leaver),
		httpapi.WithRoomMemberAuthorizer(integration.roomService),
		httpapi.WithCredentialConfig(httpapi.CredentialConfig{
			LiveKitURL:          "wss://livekit.test",
			LiveKitAPIKey:       "devkey",
			LiveKitAPISecret:    "devsecret",
			RoomSessionSecret:   wsTestSessionSecret,
			RoomSessionTokenTTL: 2 * time.Hour,
			LiveKitTokenTTL:     10 * time.Minute,
			Now:                 clock.Now,
		}),
		httpapi.WithRoomWebSocket(integration.hub),
		httpapi.WithRoomEventNotifier(integration.hub),
	)
	server := httptest.NewServer(router)
	defer server.Close()

	created := createRoomThroughHTTP(t, router, "Alice", "avatar_07")
	joined := joinRoomThroughHTTP(t, router, created.Room.InviteCode, "Bob", "avatar_08")
	hostConn := dialRoomWebSocket(t, server.URL, created.Room.ID, created.RoomSessionToken)
	defer hostConn.Close(websocket.StatusNormalClosure, "test done")
	bobConn := dialRoomWebSocket(t, server.URL, joined.Room.ID, joined.RoomSessionToken)
	assertEventType(t, readEvent(t, hostConn), "room.snapshot")
	assertEventType(t, readEvent(t, bobConn), "room.snapshot")
	if err := bobConn.Close(websocket.StatusNormalClosure, "simulate drop"); err != nil {
		t.Fatalf("bobConn.Close returned error: %v", err)
	}
	reconnectingEvent := readEvent(t, hostConn)
	assertEventType(t, reconnectingEvent, "member.reconnecting")
	var reconnectingPayload memberReconnectingPayload
	decodePayload(t, reconnectingEvent.Payload, &reconnectingPayload)
	clock.Set(reconnectingPayload.ReconnectUntil)

	timer := scheduler.Next(t)
	if timer.delay != time.Minute {
		t.Fatalf("timeout timer delay = %v, want %v", timer.delay, time.Minute)
	}
	timeoutDone := make(chan bool, 1)
	go func() {
		timeoutDone <- timer.Fire()
	}()
	select {
	case <-blockingDisconnect.entered:
	case <-time.After(time.Second):
		t.Fatalf("reconnect timeout did not reach durable disconnect")
	}

	leaveResponse := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		leaveResponse <- performAuthorizedWSJSONRequest(t, router, http.MethodPost, "/v1/rooms/"+created.Room.ID+"/leave", joined.RoomSessionToken, map[string]string{"member_id": joined.Member.ID})
	}()
	var leaveResult room.LeaveResult
	select {
	case leaveResult = <-leaver.committed:
	case <-time.After(time.Second):
		t.Fatalf("HTTP leave did not commit while timeout held the room gate")
	}
	if !leaveResult.Transitioned {
		t.Fatalf("HTTP leave Transitioned = false, want durable winner")
	}
	close(blockingDisconnect.release)
	select {
	case fired := <-timeoutDone:
		if !fired {
			t.Fatalf("manual timeout timer did not fire")
		}
	case <-time.After(time.Second):
		t.Fatalf("reconnect timeout did not finish after release")
	}
	var response *httptest.ResponseRecorder
	select {
	case response = <-leaveResponse:
	case <-time.After(time.Second):
		t.Fatalf("HTTP leave did not finish after timeout loser released room gate")
	}
	if response.Code != http.StatusNoContent {
		t.Fatalf("HTTP leave status = %d, want 204, body: %s", response.Code, response.Body.String())
	}

	terminal := readEvent(t, hostConn)
	assertEventType(t, terminal, "member.left")
	assertNoEventWithin(t, hostConn, 100*time.Millisecond)
	member, err := integration.repository.FindMemberByRoomAndID(context.Background(), created.Room.ID, joined.Member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if member.State != domain.MemberStateDisconnected {
		t.Fatalf("durable member state = %q, want disconnected", member.State)
	}
	if blockingDisconnect.Calls() != 1 {
		t.Fatalf("timeout disconnect calls = %d, want 1", blockingDisconnect.Calls())
	}
}

func TestReconnectRestoredEventUsesFreshAuthorizedMemberData(t *testing.T) {
	roomValue := wsTestRoom("room_fresh_restored", "FRESH1", domain.RoomStateActive, wsTestNow)
	staleMember := wsTestMember("mem_fresh_restored", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	staleMember.Nickname = "Stale"
	freshMember := staleMember
	freshMember.Nickname = "Fresh"
	freshMember.AvatarID = "avatar_fresh"
	freshMember.Muted = true
	hub := NewHub(Config{
		Authorizer:        fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: freshMember}},
		SnapshotStore:     fixedSnapshotStore{room: roomValue, members: []domain.Member{staleMember}},
		StateMutator:      fixedStateMutator{},
		RoomSessionSecret: wsTestSessionSecret,
		Now:               func() time.Time { return wsTestNow },
		ReconnectWindow:   time.Minute,
		WriteTimeout:      time.Second,
	})
	observer := hub.newConnection(nil, roomValue.ID, "mem_fresh_observer")
	restored := hub.newConnection(nil, roomValue.ID, freshMember.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[observer] = struct{}{}
	roomState.byMember[observer.memberID] = observer
	roomState.reconnecting[freshMember.ID] = reconnectingMember{deadline: wsTestNow.Add(time.Minute), generation: 1, phase: reconnectPhaseRestorable}
	hub.mu.Unlock()

	if err := hub.registerWithInitialSnapshot(restored); err != nil {
		t.Fatalf("registerWithInitialSnapshot returned error: %v", err)
	}
	selfOutbound := <-restored.send
	if len(selfOutbound.events) != 2 {
		t.Fatalf("restored initial group event count = %d, want 2", len(selfOutbound.events))
	}
	if selfOutbound.events[0].Type != "room.snapshot" || selfOutbound.events[1].Type != "member.restored" {
		t.Fatalf("restored initial group types = %q, %q", selfOutbound.events[0].Type, selfOutbound.events[1].Type)
	}
	if selfOutbound.events[1].Seq != selfOutbound.events[0].Seq+1 {
		t.Fatalf("restored initial group seq = %d, %d, want consecutive", selfOutbound.events[0].Seq, selfOutbound.events[1].Seq)
	}
	observerOutbound := <-observer.send
	if len(observerOutbound.events) != 1 || observerOutbound.events[0].Type != "member.restored" {
		t.Fatalf("observer restored group types = %#v, want member.restored", observerOutbound.events)
	}
	for name, event := range map[string]eventEnvelope{"self": selfOutbound.events[1], "observer": observerOutbound.events[0]} {
		payload, ok := event.Payload.(restoredMessagePayload)
		if !ok {
			t.Fatalf("%s payload type = %T, want restoredMessagePayload", name, event.Payload)
		}
		if payload.Member.Nickname != "Fresh" || payload.Member.AvatarID != "avatar_fresh" || !payload.Member.Muted {
			t.Fatalf("%s restored member = %#v, want fresh authorized data", name, payload.Member)
		}
		if payload.Member.IsSelf != (name == "self") {
			t.Fatalf("%s is_self = %v", name, payload.Member.IsSelf)
		}
	}
}

func TestReconnectRegistrationQueueSizeOneKeepsConnectionHealthy(t *testing.T) {
	roomValue := wsTestRoom("room_restore_queue_one", "QUEUE1", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_restore_queue_one", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	hub := NewHub(Config{
		Authorizer:          fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:       fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:        fixedStateMutator{},
		RoomSessionSecret:   wsTestSessionSecret,
		Now:                 func() time.Time { return wsTestNow },
		ReconnectWindow:     time.Minute,
		WriteTimeout:        time.Second,
		ConnectionQueueSize: 1,
	})
	scheduler := newManualReconnectScheduler()
	hub.afterFunc = scheduler.AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.reconnecting[member.ID] = reconnectingMember{
		deadline:   wsTestNow.Add(time.Minute),
		generation: 1,
		phase:      reconnectPhaseRestorable,
	}
	hub.mu.Unlock()

	if err := hub.registerWithInitialSnapshot(client); err != nil {
		t.Fatalf("registerWithInitialSnapshot returned error: %v", err)
	}
	if err := client.ctx.Err(); err != nil {
		t.Fatalf("queue-size-one restored connection was closed: %v", err)
	}
	hub.mu.Lock()
	current := roomState.byMember[member.ID] == client
	_, reconnecting := roomState.reconnecting[member.ID]
	hub.mu.Unlock()
	if !current {
		t.Fatalf("queue-size-one restored connection lost member ownership")
	}
	if reconnecting {
		t.Fatalf("queue-size-one restored connection re-entered reconnecting")
	}
	outbound := <-client.send
	if len(outbound.events) != 2 || outbound.events[0].Type != "room.snapshot" || outbound.events[1].Type != "member.restored" {
		t.Fatalf("queue-size-one initial group = %#v, want snapshot then restored", outbound.events)
	}
	select {
	case extra := <-client.send:
		t.Fatalf("queue-size-one restore used an extra queue slot: %#v", extra.events)
	default:
	}
}

func TestOutboundGroupQueueRemainsBoundedForSlowConsumer(t *testing.T) {
	hub := NewHub(Config{ConnectionQueueSize: 1})
	client := hub.newConnection(nil, "room_slow_group", "mem_slow_group")
	first := newOutboundGroup(
		eventEnvelope{Type: "member.speaking_changed"},
		eventEnvelope{Type: "member.reconnecting"},
	)
	if !client.enqueue(first) {
		t.Fatalf("first logical outbound group was rejected")
	}
	if client.enqueue(newOutboundGroup(eventEnvelope{Type: "member.joined"})) {
		t.Fatalf("second outbound group exceeded the configured queue capacity")
	}
	if client.ctx.Err() == nil {
		t.Fatalf("full outbound group queue did not close the slow consumer")
	}
	queued := <-client.send
	if len(queued.events) != 2 {
		t.Fatalf("queued logical group event count = %d, want 2", len(queued.events))
	}
}

type manualReconnectScheduler struct {
	scheduled chan *manualReconnectTimer
}

func newManualReconnectScheduler() *manualReconnectScheduler {
	return &manualReconnectScheduler{scheduled: make(chan *manualReconnectTimer, 32)}
}

func (s *manualReconnectScheduler) AfterFunc(delay time.Duration, callback func()) reconnectTimer {
	timer := &manualReconnectTimer{delay: delay, callback: callback}
	s.scheduled <- timer
	return timer
}

func (s *manualReconnectScheduler) Next(t *testing.T) *manualReconnectTimer {
	t.Helper()
	for {
		select {
		case timer := <-s.scheduled:
			if timer.Active() {
				return timer
			}
		case <-time.After(time.Second):
			t.Fatalf("manual reconnect scheduler has no runnable timer")
		}
	}
}

func (s *manualReconnectScheduler) RunNext(t *testing.T) time.Duration {
	t.Helper()
	timer := s.Next(t)
	if !timer.Fire() {
		t.Fatalf("manual reconnect timer became inactive before firing")
	}
	return timer.delay
}

type manualReconnectTimer struct {
	mu       sync.Mutex
	delay    time.Duration
	callback func()
	stopped  bool
	fired    bool
}

func (t *manualReconnectTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}

func (t *manualReconnectTimer) Active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return !t.stopped && !t.fired
}

func (t *manualReconnectTimer) Fire() bool {
	t.mu.Lock()
	if t.stopped || t.fired {
		t.mu.Unlock()
		return false
	}
	t.fired = true
	callback := t.callback
	t.mu.Unlock()
	callback()
	return true
}

type controlledClock struct {
	mu  sync.Mutex
	now time.Time
}

func newControlledClock(now time.Time) *controlledClock {
	return &controlledClock{now: now}
}

func (c *controlledClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *controlledClock) Set(now time.Time) {
	c.mu.Lock()
	c.now = now
	c.mu.Unlock()
}

type stateMutatorFuncs struct {
	updateMute     func(context.Context, room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error)
	updateSpeaking func(context.Context, room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error)
	disconnect     func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error)
}

func (f stateMutatorFuncs) UpdateMemberMuteContext(ctx context.Context, input room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error) {
	if f.updateMute == nil {
		return room.UpdateMemberMuteResult{}, nil
	}
	return f.updateMute(ctx, input)
}

func (f stateMutatorFuncs) UpdateMemberSpeakingContext(ctx context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
	if f.updateSpeaking == nil {
		return room.UpdateMemberSpeakingResult{}, nil
	}
	return f.updateSpeaking(ctx, input)
}

func (f stateMutatorFuncs) DisconnectMemberContext(ctx context.Context, input room.DisconnectMemberInput) (room.LeaveResult, error) {
	if f.disconnect == nil {
		return room.LeaveResult{}, nil
	}
	return f.disconnect(ctx, input)
}

type blockingDisconnectStateMutator struct {
	delegate StateMutator
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	calls    int
}

func newBlockingDisconnectStateMutator(delegate StateMutator) *blockingDisconnectStateMutator {
	return &blockingDisconnectStateMutator{
		delegate: delegate,
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
}

func (m *blockingDisconnectStateMutator) UpdateMemberMuteContext(ctx context.Context, input room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error) {
	return m.delegate.UpdateMemberMuteContext(ctx, input)
}

func (m *blockingDisconnectStateMutator) UpdateMemberSpeakingContext(ctx context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
	return m.delegate.UpdateMemberSpeakingContext(ctx, input)
}

func (m *blockingDisconnectStateMutator) DisconnectMemberContext(ctx context.Context, input room.DisconnectMemberInput) (room.LeaveResult, error) {
	m.mu.Lock()
	m.calls++
	m.mu.Unlock()
	m.once.Do(func() { close(m.entered) })
	<-m.release
	return m.delegate.DisconnectMemberContext(ctx, input)
}

func (m *blockingDisconnectStateMutator) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

type signalingRoomLeaver struct {
	delegate  *room.Service
	committed chan room.LeaveResult
}

func (l *signalingRoomLeaver) LeaveContext(ctx context.Context, input room.LeaveInput) (room.LeaveResult, error) {
	result, err := l.delegate.LeaveContext(ctx, input)
	if err == nil {
		l.committed <- result
	}
	return result, err
}

type authorizerFunc func(context.Context, room.AuthorizeMemberInput) (room.AuthorizeMemberResult, error)

func (f authorizerFunc) AuthorizeMemberContext(ctx context.Context, input room.AuthorizeMemberInput) (room.AuthorizeMemberResult, error) {
	return f(ctx, input)
}

type fixedSnapshotStore struct {
	room       domain.Room
	members    []domain.Member
	roomErr    error
	membersErr error
}

func (s fixedSnapshotStore) FindRoomByID(context.Context, string) (domain.Room, error) {
	if s.roomErr != nil {
		return domain.Room{}, s.roomErr
	}
	return s.room, nil
}

func (s fixedSnapshotStore) ListRoomMembersByStates(context.Context, string, []domain.MemberState) ([]domain.Member, error) {
	if s.membersErr != nil {
		return nil, s.membersErr
	}
	return append([]domain.Member(nil), s.members...), nil
}

type blockingSpeakingStateMutator struct {
	member  domain.Member
	entered chan struct{}
	release chan struct{}
}

func newBlockingSpeakingStateMutator(member domain.Member) *blockingSpeakingStateMutator {
	return &blockingSpeakingStateMutator{member: member, entered: make(chan struct{}), release: make(chan struct{})}
}

func (m *blockingSpeakingStateMutator) UpdateMemberMuteContext(context.Context, room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error) {
	return room.UpdateMemberMuteResult{}, nil
}

func (m *blockingSpeakingStateMutator) UpdateMemberSpeakingContext(_ context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
	member := m.member
	member.Speaking = input.Speaking
	if input.Speaking {
		close(m.entered)
		<-m.release
		return room.UpdateMemberSpeakingResult{Member: member, Changed: true}, nil
	}
	return room.UpdateMemberSpeakingResult{Member: member}, nil
}

func (m *blockingSpeakingStateMutator) DisconnectMemberContext(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error) {
	member := m.member
	member.State = domain.MemberStateDisconnected
	member.Speaking = false
	return room.LeaveResult{Member: member, Transitioned: true}, nil
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
