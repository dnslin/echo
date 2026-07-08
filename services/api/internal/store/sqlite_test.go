package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"echo/services/api/internal/domain"
	"gorm.io/gorm"
)

func TestOpenSQLiteMigratesRoomsAndMembers(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := domain.Room{
		ID:              "room_test",
		Name:            "今晚开黑",
		InviteCode:      "ABC123",
		LiveKitRoomName: "lk_room_test",
		HostAnonymousID: "anon_local_123",
		HostNickname:    "Alice",
		HostAvatarID:    "avatar_07",
		State:           domain.RoomStateActive,
		CreatedAt:       now,
		LastEmptyAt:     nil,
		ExpiresAt:       nil,
		UpdatedAt:       now,
	}
	member := domain.Member{
		ID:              "mem_test",
		RoomID:          room.ID,
		AnonymousID:     room.HostAnonymousID,
		Nickname:        room.HostNickname,
		AvatarID:        room.HostAvatarID,
		IsHost:          true,
		State:           domain.MemberStateOnline,
		Muted:           false,
		Speaking:        false,
		VoiceMode:       domain.VoiceModePushToTalk,
		JoinedAt:        now,
		LiveKitIdentity: "mem_test",
	}

	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	var storedRoom RoomModel
	if err := db.First(&storedRoom, "id = ?", room.ID).Error; err != nil {
		t.Fatalf("stored room not found: %v", err)
	}
	if storedRoom.InviteCode != room.InviteCode || storedRoom.State != string(domain.RoomStateActive) {
		t.Fatalf("stored room = %#v, want invite %q and active state", storedRoom, room.InviteCode)
	}
	if storedRoom.LastEmptyAt != nil {
		t.Fatalf("stored room LastEmptyAt = %v, want nil", storedRoom.LastEmptyAt)
	}
	if storedRoom.ExpiresAt != nil {
		t.Fatalf("stored room ExpiresAt = %v, want nil", storedRoom.ExpiresAt)
	}

	var storedMember MemberModel
	if err := db.First(&storedMember, "id = ?", member.ID).Error; err != nil {
		t.Fatalf("stored member not found: %v", err)
	}
	if !storedMember.IsHost || storedMember.State != string(domain.MemberStateOnline) || storedMember.Muted || storedMember.Speaking || storedMember.VoiceMode != string(domain.VoiceModePushToTalk) {
		t.Fatalf("stored member initial state = %#v, want host online unmuted not speaking push_to_talk", storedMember)
	}
}

func TestCreateRoomWithMemberReturnsInviteConflictForDuplicateInviteCode(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

	firstRoom := testRoom("room_one", "ABC123", now)
	firstMember := testMember("mem_one", firstRoom.ID, now)
	if err := repository.CreateRoomWithMember(context.Background(), firstRoom, firstMember); err != nil {
		t.Fatalf("first CreateRoomWithMember returned error: %v", err)
	}

	secondRoom := testRoom("room_two", "ABC123", now)
	secondMember := testMember("mem_two", secondRoom.ID, now)
	err := repository.CreateRoomWithMember(context.Background(), secondRoom, secondMember)
	if !errors.Is(err, domain.ErrInviteCodeConflict) {
		t.Fatalf("duplicate invite error = %v, want ErrInviteCodeConflict", err)
	}
}

func openTestSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := OpenSQLite(filepath.Join(t.TempDir(), "echo.sqlite3"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func testRoom(id string, inviteCode string, now time.Time) domain.Room {
	return domain.Room{
		ID:              id,
		Name:            "今晚开黑",
		InviteCode:      inviteCode,
		LiveKitRoomName: "lk_" + id,
		HostAnonymousID: "anon_local_123",
		HostNickname:    "Alice",
		HostAvatarID:    "avatar_07",
		State:           domain.RoomStateActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

func testMember(id string, roomID string, now time.Time) domain.Member {
	return domain.Member{
		ID:              id,
		RoomID:          roomID,
		AnonymousID:     "anon_local_123",
		Nickname:        "Alice",
		AvatarID:        "avatar_07",
		IsHost:          true,
		State:           domain.MemberStateOnline,
		Muted:           false,
		Speaking:        false,
		VoiceMode:       domain.VoiceModePushToTalk,
		JoinedAt:        now,
		LiveKitIdentity: id,
	}
}
