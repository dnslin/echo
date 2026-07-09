package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

func TestFindRoomByIDReturnsPersistedRoom(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_credentials", "CRED01", now)
	if err := repository.CreateRoomWithMember(context.Background(), room, testMember("mem_credentials", room.ID, now)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	found, err := repository.FindRoomByID(context.Background(), room.ID)
	if err != nil {
		t.Fatalf("FindRoomByID returned error: %v", err)
	}
	if found.ID != room.ID || found.InviteCode != room.InviteCode || found.LiveKitRoomName != room.LiveKitRoomName {
		t.Fatalf("found room = %#v, want persisted room %#v", found, room)
	}
}

func TestFindRoomByIDReturnsRoomNotFound(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)

	_, err := repository.FindRoomByID(context.Background(), "room_missing")
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("FindRoomByID missing error = %v, want ErrRoomNotFound", err)
	}
}

func TestFindMemberByRoomAndIDReturnsPersistedMember(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_credentials", "CRED02", now)
	member := testMember("mem_member_credentials", room.ID, now)
	member.State = domain.MemberStateReconnecting
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if found.ID != member.ID || found.RoomID != room.ID || found.State != domain.MemberStateReconnecting || found.LiveKitIdentity != member.LiveKitIdentity {
		t.Fatalf("found member = %#v, want persisted member %#v", found, member)
	}
}

func TestFindMemberByRoomAndIDRejectsMissingOrWrongRoomMember(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_owner", "CRED03", now)
	if err := repository.CreateRoomWithMember(context.Background(), room, testMember("mem_member_owner", room.ID, now)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	tests := []struct {
		name     string
		roomID   string
		memberID string
	}{
		{name: "missing member", roomID: room.ID, memberID: "mem_missing"},
		{name: "wrong room", roomID: "room_other", memberID: "mem_member_owner"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := repository.FindMemberByRoomAndID(context.Background(), tt.roomID, tt.memberID)
			if !errors.Is(err, domain.ErrMemberNotFound) {
				t.Fatalf("FindMemberByRoomAndID error = %v, want ErrMemberNotFound", err)
			}
		})
	}
}
