package ws

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"echo/services/api/internal/domain"
	"echo/services/api/internal/room"
	"github.com/coder/websocket"
)

func TestMuteQueueSizeOneDeliversSpeakingClearAndMuteAsOneOrderedGroup(t *testing.T) {
	roomValue := wsTestRoom("room_mute_queue_one", "MUTEQ1", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_mute_queue_one", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
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
		ReconnectWindow:     time.Minute,
		WriteTimeout:        time.Second,
		ConnectionQueueSize: 1,
	})
	hub.afterFunc = newManualReconnectScheduler().AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.byMember[member.ID] = client
	hub.mu.Unlock()
	t.Cleanup(func() { stopReconnectTimer(roomState, hub, member.ID) })

	client.handleMuteChanged(json.RawMessage(`{"muted":true}`), nil)

	if err := client.ctx.Err(); err != nil {
		t.Fatalf("queue-size-one mute closed a healthy connection: %v", err)
	}
	outbound := <-client.send
	assertOutboundTypes(t, outbound, "member.speaking_changed", "member.muted_changed")
	if outbound.events[1].Seq != outbound.events[0].Seq+1 {
		t.Fatalf("mute group seq = %d, %d, want consecutive", outbound.events[0].Seq, outbound.events[1].Seq)
	}
	assertNoOutboundGroup(t, client)
}

func TestUnexpectedDisconnectQueueSizeOneDeliversSpeakingClearAndReconnectAsOneOrderedGroup(t *testing.T) {
	roomValue := wsTestRoom("room_disconnect_queue_one", "DROPQ1", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_disconnect_queue_one", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	member.Speaking = true
	mutator := stateMutatorFuncs{
		updateSpeaking: func(_ context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
			member.Speaking = input.Speaking
			return room.UpdateMemberSpeakingResult{Member: member, Changed: true}, nil
		},
	}
	hub := NewHub(Config{
		Authorizer:          fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:       fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:        mutator,
		RoomSessionSecret:   wsTestSessionSecret,
		Now:                 func() time.Time { return wsTestNow },
		ReconnectWindow:     time.Minute,
		WriteTimeout:        time.Second,
		ConnectionQueueSize: 1,
	})
	hub.afterFunc = newManualReconnectScheduler().AfterFunc
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	observer := hub.newConnection(nil, roomValue.ID, "mem_disconnect_observer")
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.connections[observer] = struct{}{}
	roomState.byMember[member.ID] = client
	roomState.byMember[observer.memberID] = observer
	roomState.lastSpeakingAccepted[member.ID] = wsTestNow
	hub.mu.Unlock()
	t.Cleanup(func() { stopReconnectTimer(roomState, hub, member.ID) })

	hub.handleUnexpectedDisconnect(client)

	if err := observer.ctx.Err(); err != nil {
		t.Fatalf("queue-size-one reconnect transition closed a healthy observer: %v", err)
	}
	outbound := <-observer.send
	assertOutboundTypes(t, outbound, "member.speaking_changed", "member.reconnecting")
	if outbound.events[1].Seq != outbound.events[0].Seq+1 {
		t.Fatalf("reconnect group seq = %d, %d, want consecutive", outbound.events[0].Seq, outbound.events[1].Seq)
	}
	assertNoOutboundGroup(t, observer)
	hub.mu.Lock()
	_, retainedThrottle := roomState.lastSpeakingAccepted[member.ID]
	hub.mu.Unlock()
	if retainedThrottle {
		t.Fatalf("server-forced reconnect speaking clear retained client throttle timestamp")
	}
}

func TestMuteForcedSpeakingClearDoesNotThrottleImmediateClientTrue(t *testing.T) {
	now := wsTestNow
	roomValue := wsTestRoom("room_forced_false_throttle", "THROTT", domain.RoomStateActive, now)
	member := wsTestMember("mem_forced_false_throttle", roomValue.ID, domain.MemberStateOnline, false, now)
	acceptedSpeakingReports := 0
	mutator := stateMutatorFuncs{
		updateMute: func(_ context.Context, input room.UpdateMemberMuteInput) (room.UpdateMemberMuteResult, error) {
			mutedChanged := member.Muted != input.Muted
			speakingChanged := input.Muted && member.Speaking
			member.Muted = input.Muted
			if speakingChanged {
				member.Speaking = false
			}
			return room.UpdateMemberMuteResult{Member: member, MutedChanged: mutedChanged, SpeakingChanged: speakingChanged}, nil
		},
		updateSpeaking: func(_ context.Context, input room.UpdateMemberSpeakingInput) (room.UpdateMemberSpeakingResult, error) {
			if member.Speaking == input.Speaking {
				return room.UpdateMemberSpeakingResult{Member: member}, nil
			}
			member.Speaking = input.Speaking
			acceptedSpeakingReports++
			return room.UpdateMemberSpeakingResult{Member: member, Changed: true}, nil
		},
	}
	hub := NewHub(Config{
		Authorizer:          fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:       fixedSnapshotStore{room: roomValue, members: []domain.Member{member}},
		StateMutator:        mutator,
		RoomSessionSecret:   wsTestSessionSecret,
		Now:                 func() time.Time { return now },
		WriteTimeout:        time.Second,
		ConnectionQueueSize: 1,
		SpeakingMinInterval: time.Minute,
	})
	client := hub.newConnection(nil, roomValue.ID, member.ID)
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[client] = struct{}{}
	roomState.byMember[member.ID] = client
	hub.mu.Unlock()

	client.handleSpeakingChanged(json.RawMessage(`{"speaking":true}`), nil)
	assertOutboundTypes(t, <-client.send, "member.speaking_changed")
	client.handleMuteChanged(json.RawMessage(`{"muted":true}`), nil)
	assertOutboundTypes(t, <-client.send, "member.speaking_changed", "member.muted_changed")
	client.handleMuteChanged(json.RawMessage(`{"muted":false}`), nil)
	assertOutboundTypes(t, <-client.send, "member.muted_changed")

	now = now.Add(time.Millisecond)
	client.handleSpeakingChanged(json.RawMessage(`{"speaking":true}`), nil)

	if acceptedSpeakingReports != 2 {
		t.Fatalf("accepted speaking reports = %d, want initial and immediate post-unmute true", acceptedSpeakingReports)
	}
	outbound := <-client.send
	assertOutboundTypes(t, outbound, "member.speaking_changed")
	payload := outbound.events[0].Payload.(speakingChangedMessagePayload)
	if !payload.Speaking {
		t.Fatalf("post-unmute speaking payload = false, want true")
	}
}

func TestSnapshotFailureQueuesErrorAndCloseInOneGroup(t *testing.T) {
	roomValue := wsTestRoom("room_snapshot_error_group", "ERRGRP", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_snapshot_error_group", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
	hub := NewHub(Config{
		Authorizer:          fixedAuthorizer{result: room.AuthorizeMemberResult{Room: roomValue, Member: member}},
		SnapshotStore:       fixedSnapshotStore{roomErr: errors.New("snapshot unavailable")},
		StateMutator:        fixedStateMutator{},
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
	hub.mu.Unlock()

	client.sendSnapshot()

	if err := client.ctx.Err(); err != nil {
		t.Fatalf("snapshot error plus close exhausted one-slot queue: %v", err)
	}
	outbound := <-client.send
	assertOutboundTypes(t, outbound, "room.error")
	if !outbound.closeAfter || outbound.closeCode != websocket.StatusInternalError {
		t.Fatalf("snapshot failure close metadata = after:%v code:%v", outbound.closeAfter, outbound.closeCode)
	}
	assertNoOutboundGroup(t, client)
}

func TestPermanentLeaveClearsMemberTransientState(t *testing.T) {
	roomValue := wsTestRoom("room_leave_cleanup", "LEAVCL", domain.RoomStateActive, wsTestNow)
	member := wsTestMember("mem_leave_cleanup", roomValue.ID, domain.MemberStateDisconnected, false, wsTestNow)
	hub := NewHub(Config{Now: func() time.Time { return wsTestNow }, ConnectionQueueSize: 1})
	observer := hub.newConnection(nil, roomValue.ID, "mem_leave_cleanup_observer")
	timer := &manualReconnectTimer{}
	hub.mu.Lock()
	roomState := hub.roomStateLocked(roomValue.ID)
	roomState.connections[observer] = struct{}{}
	roomState.byMember[observer.memberID] = observer
	roomState.reconnecting[member.ID] = reconnectingMember{
		deadline:   wsTestNow.Add(time.Minute),
		generation: 1,
		timer:      timer,
		phase:      reconnectPhaseRestorable,
	}
	roomState.lastSpeakingAccepted[member.ID] = wsTestNow
	hub.mu.Unlock()
	defer observer.closeWithMode(closeModeNoReconnect, websocket.StatusNormalClosure, "test done")

	hub.NotifyMemberLeft(context.Background(), roomValue, member)

	hub.mu.Lock()
	_, retainedReconnect := roomState.reconnecting[member.ID]
	_, retainedThrottle := roomState.lastSpeakingAccepted[member.ID]
	hub.mu.Unlock()
	if retainedReconnect || retainedThrottle {
		t.Fatalf("permanent leave retained reconnect/throttle state: %v/%v", retainedReconnect, retainedThrottle)
	}
	if timer.Active() {
		t.Fatalf("permanent leave retained active reconnect timer")
	}
	assertOutboundTypes(t, <-observer.send, "member.left")
}

func TestReconnectTerminalOutcomesClearMemberTransientState(t *testing.T) {
	tests := []struct {
		name          string
		disconnect    func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error)
		wantBroadcast bool
	}{
		{
			name: "timeout wins durable transition",
			disconnect: func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error) {
				return room.LeaveResult{Transitioned: true}, nil
			},
			wantBroadcast: true,
		},
		{
			name: "stable competing terminal already won",
			disconnect: func(context.Context, room.DisconnectMemberInput) (room.LeaveResult, error) {
				return room.LeaveResult{}, room.ErrMemberNotActive
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			roomValue := wsTestRoom("room_timeout_cleanup", "TIMECL", domain.RoomStateActive, wsTestNow)
			member := wsTestMember("mem_timeout_cleanup", roomValue.ID, domain.MemberStateOnline, false, wsTestNow)
			hub := NewHub(Config{
				StateMutator:        stateMutatorFuncs{disconnect: tt.disconnect},
				Now:                 func() time.Time { return wsTestNow },
				ConnectionQueueSize: 1,
			})
			observer := hub.newConnection(nil, roomValue.ID, "mem_timeout_cleanup_observer")
			timer := &manualReconnectTimer{}
			hub.mu.Lock()
			roomState := hub.roomStateLocked(roomValue.ID)
			roomState.connections[observer] = struct{}{}
			roomState.byMember[observer.memberID] = observer
			roomState.reconnecting[member.ID] = reconnectingMember{
				deadline:   wsTestNow,
				generation: 1,
				timer:      timer,
				phase:      reconnectPhaseTimingOut,
			}
			roomState.lastSpeakingAccepted[member.ID] = wsTestNow
			hub.mu.Unlock()
			defer observer.closeWithMode(closeModeNoReconnect, websocket.StatusNormalClosure, "test done")

			hub.handleReconnectTimeout(roomValue.ID, member.ID, 1)

			hub.mu.Lock()
			_, retainedReconnect := roomState.reconnecting[member.ID]
			_, retainedThrottle := roomState.lastSpeakingAccepted[member.ID]
			hub.mu.Unlock()
			if retainedReconnect || retainedThrottle {
				t.Fatalf("terminal outcome retained reconnect/throttle state: %v/%v", retainedReconnect, retainedThrottle)
			}
			if timer.Active() {
				t.Fatalf("terminal outcome retained active reconnect timer")
			}
			if tt.wantBroadcast {
				assertOutboundTypes(t, <-observer.send, "member.disconnected")
			} else {
				assertNoOutboundGroup(t, observer)
			}
		})
	}
}

func TestClosedConnectionNeverAcceptsOutboundGroup(t *testing.T) {
	hub := NewHub(Config{Now: func() time.Time { return wsTestNow }, ConnectionQueueSize: 1})
	outbound := newOutboundGroup(eventEnvelope{Type: "member.joined", Seq: 1, SentAt: wsTestNow, Payload: struct{}{}})

	for attempt := 0; attempt < 256; attempt++ {
		client := hub.newConnection(nil, "room_closed_queue", "mem_closed_queue")
		close(client.done)
		if client.enqueueLocked(outbound) {
			t.Fatalf("closed connection accepted outbound group on attempt %d", attempt)
		}
		if len(client.send) != 0 {
			t.Fatalf("closed connection queue length = %d, want 0", len(client.send))
		}
		client.cancel()
	}
}

func TestOutboundGroupQueueClosesConsumerOnlyAfterMultipleGroupsAccumulate(t *testing.T) {
	hub := NewHub(Config{
		Now:                 func() time.Time { return wsTestNow },
		ConnectionQueueSize: 1,
	})
	client := hub.newConnection(nil, "room_slow_group", "mem_slow_group")
	first := newOutboundGroup(eventEnvelope{Type: "member.joined", Seq: 1, SentAt: wsTestNow, Payload: struct{}{}})
	second := newOutboundGroup(eventEnvelope{Type: "member.joined", Seq: 2, SentAt: wsTestNow, Payload: struct{}{}})

	if !client.enqueue(first) {
		t.Fatalf("first outbound group was rejected")
	}
	if client.enqueue(second) {
		t.Fatalf("second outbound group was accepted into a full queue")
	}
	if err := client.ctx.Err(); err == nil {
		t.Fatalf("full outbound group queue did not close the slow consumer")
	}
	queued := <-client.send
	assertOutboundTypes(t, queued, "member.joined")
	if queued.events[0].Seq != 1 {
		t.Fatalf("queued event seq = %d, want first group seq 1", queued.events[0].Seq)
	}
	assertNoOutboundGroup(t, client)
}

func assertOutboundTypes(t *testing.T, outbound outboundGroup, want ...string) {
	t.Helper()
	if len(outbound.events) != len(want) {
		t.Fatalf("outbound event count = %d, want %d: %#v", len(outbound.events), len(want), outbound.events)
	}
	for index, eventType := range want {
		if outbound.events[index].Type != eventType {
			t.Fatalf("outbound event[%d] type = %q, want %q", index, outbound.events[index].Type, eventType)
		}
	}
}

func assertNoOutboundGroup(t *testing.T, client *connection) {
	t.Helper()
	select {
	case outbound := <-client.send:
		t.Fatalf("unexpected extra outbound group: %#v", outbound.events)
	default:
	}
}

func stopReconnectTimer(roomState *roomConnections, hub *Hub, memberID string) {
	hub.mu.Lock()
	reconnecting := roomState.reconnecting[memberID]
	hub.mu.Unlock()
	if reconnecting.timer != nil {
		reconnecting.timer.Stop()
	}
}
