package store

import (
	"context"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

func TestUpdateMemberMutePersistsMutedAndClearsSpeaking(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_mute", "MUT301", createdAt)
	member := testMember("mem_member_mute", room.ID, createdAt)
	member.Speaking = true
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	if err := repository.UpdateMemberMute(context.Background(), room.ID, member.ID, true); err != nil {
		t.Fatalf("UpdateMemberMute returned error: %v", err)
	}

	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if !found.Muted {
		t.Fatalf("found member muted = %v, want true", found.Muted)
	}
	if found.Speaking {
		t.Fatalf("found member speaking = %v, want false after mute", found.Speaking)
	}
}

func TestUpdateMemberSpeakingPersistsSpeakingRoundTrip(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_speaking", "SPK301", createdAt)
	member := testMember("mem_member_speaking", room.ID, createdAt)
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	if err := repository.UpdateMemberSpeaking(context.Background(), room.ID, member.ID, true); err != nil {
		t.Fatalf("UpdateMemberSpeaking(true) returned error: %v", err)
	}
	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID after true returned error: %v", err)
	}
	if !found.Speaking {
		t.Fatalf("found member speaking after true = %v, want true", found.Speaking)
	}

	if err := repository.UpdateMemberSpeaking(context.Background(), room.ID, member.ID, false); err != nil {
		t.Fatalf("UpdateMemberSpeaking(false) returned error: %v", err)
	}
	found, err = repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID after false returned error: %v", err)
	}
	if found.Speaking {
		t.Fatalf("found member speaking after false = %v, want false", found.Speaking)
	}
}

func TestDisconnectMemberReusesLeaveLifecycleSemantics(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	disconnectedAt := createdAt.Add(5 * time.Minute)
	room := testRoom("room_disconnect_member", "DSC301", createdAt)
	member := testMember("mem_disconnect_member", room.ID, createdAt)
	member.Speaking = true
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	disconnectedRoom, disconnectedMember, err := repository.DisconnectMember(context.Background(), room.ID, member.ID, activeMemberStates(), disconnectedAt, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("DisconnectMember returned error: %v", err)
	}

	if disconnectedMember.State != domain.MemberStateDisconnected {
		t.Fatalf("disconnected member state = %q, want disconnected", disconnectedMember.State)
	}
	if disconnectedMember.Speaking {
		t.Fatalf("disconnected member speaking = %v, want false", disconnectedMember.Speaking)
	}
	assertRoomRetentionStarted(t, disconnectedRoom, disconnectedAt)
}
