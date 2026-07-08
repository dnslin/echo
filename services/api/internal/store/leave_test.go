package store

import (
	"context"
	"errors"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

const testEmptyRoomRetention = 30 * time.Minute

func TestLeaveRoomMemberMarksMemberDisconnectedAndExcludesFromActiveCount(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	leftAt := createdAt.Add(5 * time.Minute)
	room := testRoom("room_leave_state", "LEAV01", createdAt)
	member := testMember("mem_leave_state", room.ID, createdAt)
	member.Speaking = true
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	_, leftMember, err := repository.LeaveRoomMember(context.Background(), room.ID, member.ID, activeMemberStates(), leftAt, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("LeaveRoomMember returned error: %v", err)
	}

	if leftMember.State != domain.MemberStateDisconnected || leftMember.Speaking {
		t.Fatalf("returned member state/speaking = %q/%v, want disconnected/false", leftMember.State, leftMember.Speaking)
	}
	var stored MemberModel
	if err := db.First(&stored, "id = ?", member.ID).Error; err != nil {
		t.Fatalf("left member not found: %v", err)
	}
	if stored.State != string(domain.MemberStateDisconnected) || stored.Speaking {
		t.Fatalf("stored member state/speaking = %q/%v, want disconnected/false", stored.State, stored.Speaking)
	}
	count, err := repository.CountRoomMembersByStates(context.Background(), room.ID, activeMemberStates())
	if err != nil {
		t.Fatalf("CountRoomMembersByStates returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("active member count after leave = %d, want 0", count)
	}
}

func TestLeaveRoomMemberWithOtherActiveMembersDoesNotStartRetention(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	leftAt := createdAt.Add(5 * time.Minute)
	room := testRoom("room_leave_nonlast", "LEAV02", createdAt)
	host := testMember("mem_leave_host", room.ID, createdAt)
	if err := repository.CreateRoomWithMember(context.Background(), room, host); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	guest := joinMember("mem_leave_guest", room.ID, domain.MemberStateOnline, createdAt.Add(time.Minute))
	guest.Speaking = true
	if err := repository.CreateMember(context.Background(), guest); err != nil {
		t.Fatalf("CreateMember returned error: %v", err)
	}

	leftRoom, leftMember, err := repository.LeaveRoomMember(context.Background(), room.ID, guest.ID, activeMemberStates(), leftAt, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("LeaveRoomMember returned error: %v", err)
	}

	if leftMember.State != domain.MemberStateDisconnected || leftMember.Speaking {
		t.Fatalf("left member state/speaking = %q/%v, want disconnected/false", leftMember.State, leftMember.Speaking)
	}
	if leftRoom.LastEmptyAt != nil || leftRoom.ExpiresAt != nil {
		t.Fatalf("returned room last_empty_at/expires_at = %v/%v, want nil/nil", leftRoom.LastEmptyAt, leftRoom.ExpiresAt)
	}
	found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}
	if found.LastEmptyAt != nil || found.ExpiresAt != nil {
		t.Fatalf("persisted room last_empty_at/expires_at = %v/%v, want nil/nil", found.LastEmptyAt, found.ExpiresAt)
	}
	count, err := repository.CountRoomMembersByStates(context.Background(), room.ID, activeMemberStates())
	if err != nil {
		t.Fatalf("CountRoomMembersByStates returned error: %v", err)
	}
	if count != 1 {
		t.Fatalf("active member count after non-last leave = %d, want 1", count)
	}
}

func TestLeaveRoomMemberLastActiveMemberStartsRetention(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	leftAt := createdAt.Add(5 * time.Minute)
	room := testRoom("room_leave_last", "LEAV03", createdAt)
	member := testMember("mem_leave_last", room.ID, createdAt)
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	leftRoom, _, err := repository.LeaveRoomMember(context.Background(), room.ID, member.ID, activeMemberStates(), leftAt, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("LeaveRoomMember returned error: %v", err)
	}

	assertRoomRetentionStarted(t, leftRoom, leftAt)
	found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}
	assertRoomRetentionStarted(t, found, leftAt)
}

func TestLeaveRoomMemberRepeatedLeaveDoesNotExtendRetention(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	firstLeaveAt := createdAt.Add(5 * time.Minute)
	secondLeaveAt := firstLeaveAt.Add(10 * time.Minute)
	room := testRoom("room_leave_repeat", "LEAV04", createdAt)
	member := testMember("mem_leave_repeat", room.ID, createdAt)
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	if _, _, err := repository.LeaveRoomMember(context.Background(), room.ID, member.ID, activeMemberStates(), firstLeaveAt, testEmptyRoomRetention); err != nil {
		t.Fatalf("first LeaveRoomMember returned error: %v", err)
	}

	leftRoom, leftMember, err := repository.LeaveRoomMember(context.Background(), room.ID, member.ID, activeMemberStates(), secondLeaveAt, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("second LeaveRoomMember returned error: %v", err)
	}

	if leftMember.State != domain.MemberStateDisconnected || leftMember.Speaking {
		t.Fatalf("left member state/speaking = %q/%v, want disconnected/false", leftMember.State, leftMember.Speaking)
	}
	assertRoomRetentionStarted(t, leftRoom, firstLeaveAt)
	found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}
	assertRoomRetentionStarted(t, found, firstLeaveAt)
}

func TestLeaveRoomMemberLastActiveMemberRefreshesStaleOrPartialRetentionMetadata(t *testing.T) {
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	oldEmptyAt := createdAt.Add(5 * time.Minute)
	oldExpiresAt := oldEmptyAt.Add(testEmptyRoomRetention)
	leftAt := oldExpiresAt.Add(5 * time.Minute)
	tests := []struct {
		name        string
		roomID      string
		inviteCode  string
		lastEmptyAt *time.Time
		expiresAt   *time.Time
	}{
		{
			name:        "stale complete metadata",
			roomID:      "room_leave_stale_retention",
			inviteCode:  "LEAVSR",
			lastEmptyAt: &oldEmptyAt,
			expiresAt:   &oldExpiresAt,
		},
		{
			name:        "partial missing expiry metadata",
			roomID:      "room_leave_partial_retention",
			inviteCode:  "LEAVPR",
			lastEmptyAt: &oldEmptyAt,
			expiresAt:   nil,
		},
		{
			name:        "partial missing empty timestamp metadata",
			roomID:      "room_leave_partial_empty_at",
			inviteCode:  "LEAVPE",
			lastEmptyAt: nil,
			expiresAt:   &oldExpiresAt,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestSQLite(t)
			repository := NewRepository(db)
			room := testRoom(tt.roomID, tt.inviteCode, createdAt)
			room.LastEmptyAt = tt.lastEmptyAt
			room.ExpiresAt = tt.expiresAt
			room.UpdatedAt = oldEmptyAt
			member := testMember("mem_"+tt.roomID, room.ID, createdAt)
			member.State = domain.MemberStateReconnecting
			if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
				t.Fatalf("CreateRoomWithMember returned error: %v", err)
			}

			leftRoom, _, err := repository.LeaveRoomMember(context.Background(), room.ID, member.ID, activeMemberStates(), leftAt, testEmptyRoomRetention)
			if err != nil {
				t.Fatalf("LeaveRoomMember returned error: %v", err)
			}

			assertRoomRetentionStarted(t, leftRoom, leftAt)
			found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
			if err != nil {
				t.Fatalf("FindRoomByInviteCode returned error: %v", err)
			}
			assertRoomRetentionStarted(t, found, leftAt)
		})
	}
}

func TestLeaveRoomMemberRepeatedLeaveAfterRetentionExpiresReturnsRoomExpired(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	lastEmptyAt := createdAt.Add(5 * time.Minute)
	expiresAt := lastEmptyAt.Add(testEmptyRoomRetention)
	leftAt := expiresAt.Add(time.Second)
	room := testRoom("room_leave_repeat_expired", "LEAVEX", createdAt)
	room.LastEmptyAt = &lastEmptyAt
	room.ExpiresAt = &expiresAt
	room.UpdatedAt = lastEmptyAt
	member := testMember("mem_leave_repeat_expired", room.ID, createdAt)
	member.State = domain.MemberStateDisconnected
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	_, _, err := repository.LeaveRoomMember(context.Background(), room.ID, member.ID, activeMemberStates(), leftAt, testEmptyRoomRetention)
	if !errors.Is(err, domain.ErrRoomExpired) {
		t.Fatalf("LeaveRoomMember error = %v, want ErrRoomExpired", err)
	}
	found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}
	if found.State != domain.RoomStateExpired || !found.UpdatedAt.Equal(leftAt) {
		t.Fatalf("room state/updated_at = %q/%v, want expired/%v", found.State, found.UpdatedAt, leftAt)
	}
	if found.LastEmptyAt == nil || !found.LastEmptyAt.Equal(lastEmptyAt) {
		t.Fatalf("room last_empty_at = %v, want unchanged %v", found.LastEmptyAt, lastEmptyAt)
	}
	if found.ExpiresAt == nil || !found.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("room expires_at = %v, want unchanged %v", found.ExpiresAt, expiresAt)
	}
}

func TestLeaveRoomMemberReturnsStableMissingAndExpiredErrors(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	leftAt := createdAt.Add(5 * time.Minute)
	room := testRoom("room_leave_errors", "LEAV05", createdAt)
	if err := repository.CreateRoomWithMember(context.Background(), room, testMember("mem_leave_errors", room.ID, createdAt)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	_, _, err := repository.LeaveRoomMember(context.Background(), "room_missing", "mem_leave_errors", activeMemberStates(), leftAt, testEmptyRoomRetention)
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("missing room error = %v, want ErrRoomNotFound", err)
	}
	_, _, err = repository.LeaveRoomMember(context.Background(), room.ID, "mem_missing", activeMemberStates(), leftAt, testEmptyRoomRetention)
	if !errors.Is(err, domain.ErrMemberNotFound) {
		t.Fatalf("missing member error = %v, want ErrMemberNotFound", err)
	}
	if err := repository.MarkRoomExpired(context.Background(), room.ID, leftAt); err != nil {
		t.Fatalf("MarkRoomExpired returned error: %v", err)
	}
	_, _, err = repository.LeaveRoomMember(context.Background(), room.ID, "mem_leave_errors", activeMemberStates(), leftAt, testEmptyRoomRetention)
	if !errors.Is(err, domain.ErrRoomExpired) {
		t.Fatalf("expired room error = %v, want ErrRoomExpired", err)
	}
}

func TestExpireEmptyRoomsMarksDueRetainedRoomsExpired(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 30, 0, 0, time.UTC)
	createdAt := now.Add(-time.Hour)
	lastEmptyAt := now.Add(-testEmptyRoomRetention)
	expiresAt := now
	room := testRoom("room_due_expiry", "DUE030", createdAt)
	room.LastEmptyAt = &lastEmptyAt
	room.ExpiresAt = &expiresAt
	room.UpdatedAt = lastEmptyAt
	member := testMember("mem_due_expiry", room.ID, createdAt)
	member.State = domain.MemberStateDisconnected
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	expired, err := repository.ExpireEmptyRooms(context.Background(), now, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("ExpireEmptyRooms returned error: %v", err)
	}

	if expired != 1 {
		t.Fatalf("expired room count = %d, want 1", expired)
	}
	found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}
	if found.State != domain.RoomStateExpired || !found.UpdatedAt.Equal(now) {
		t.Fatalf("expired room state/updated_at = %q/%v, want expired/%v", found.State, found.UpdatedAt, now)
	}
}

func TestExpireEmptyRoomsDoesNotExpireDueRetainedRoomWithActiveMembers(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 30, 0, 0, time.UTC)
	createdAt := now.Add(-time.Hour)
	lastEmptyAt := now.Add(-testEmptyRoomRetention)
	expiresAt := now
	room := testRoom("room_due_active", "DUEACT", createdAt)
	room.LastEmptyAt = &lastEmptyAt
	room.ExpiresAt = &expiresAt
	room.UpdatedAt = lastEmptyAt
	member := testMember("mem_due_active", room.ID, createdAt)
	member.State = domain.MemberStateReconnecting
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	expired, err := repository.ExpireEmptyRooms(context.Background(), now, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("ExpireEmptyRooms returned error: %v", err)
	}

	if expired != 0 {
		t.Fatalf("expired room count = %d, want 0", expired)
	}
	found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}
	if found.State != domain.RoomStateActive {
		t.Fatalf("due retained active-member room state = %q, want active", found.State)
	}
	if found.ExpiresAt == nil || !found.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("due retained active-member room expires_at = %v, want %v", found.ExpiresAt, expiresAt)
	}
}

func TestExpireEmptyRoomsExpiresOldNoActiveRoomsWithoutBreakingActiveRooms(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 30, 0, 0, time.UTC)
	oldCreatedAt := now.Add(-testEmptyRoomRetention)
	oldEmptyRoom := testRoom("room_old_empty", "OLD030", oldCreatedAt)
	oldEmptyMember := testMember("mem_old_empty", oldEmptyRoom.ID, oldCreatedAt)
	oldEmptyMember.State = domain.MemberStateDisconnected
	if err := repository.CreateRoomWithMember(context.Background(), oldEmptyRoom, oldEmptyMember); err != nil {
		t.Fatalf("CreateRoomWithMember old empty returned error: %v", err)
	}
	oldActiveRoom := testRoom("room_old_active", "ACTOLD", oldCreatedAt)
	oldActiveMember := testMember("mem_old_active", oldActiveRoom.ID, oldCreatedAt)
	if err := repository.CreateRoomWithMember(context.Background(), oldActiveRoom, oldActiveMember); err != nil {
		t.Fatalf("CreateRoomWithMember old active returned error: %v", err)
	}

	expired, err := repository.ExpireEmptyRooms(context.Background(), now, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("ExpireEmptyRooms returned error: %v", err)
	}

	if expired != 1 {
		t.Fatalf("expired room count = %d, want 1", expired)
	}
	oldEmptyFound, err := repository.FindRoomByInviteCode(context.Background(), oldEmptyRoom.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode old empty returned error: %v", err)
	}
	if oldEmptyFound.State != domain.RoomStateExpired {
		t.Fatalf("old no-active room state = %q, want expired", oldEmptyFound.State)
	}
	oldActiveFound, err := repository.FindRoomByInviteCode(context.Background(), oldActiveRoom.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode old active returned error: %v", err)
	}
	if oldActiveFound.State != domain.RoomStateActive {
		t.Fatalf("old active room state = %q, want active", oldActiveFound.State)
	}
	if oldActiveFound.LastEmptyAt != nil || oldActiveFound.ExpiresAt != nil {
		t.Fatalf("old active room last_empty_at/expires_at = %v/%v, want nil/nil", oldActiveFound.LastEmptyAt, oldActiveFound.ExpiresAt)
	}
}

func assertRoomRetentionStarted(t *testing.T, room domain.Room, leftAt time.Time) {
	t.Helper()
	if room.State != domain.RoomStateActive {
		t.Fatalf("room state = %q, want active", room.State)
	}
	if room.LastEmptyAt == nil || !room.LastEmptyAt.Equal(leftAt) {
		t.Fatalf("room last_empty_at = %v, want %v", room.LastEmptyAt, leftAt)
	}
	wantExpiresAt := leftAt.Add(testEmptyRoomRetention)
	if room.ExpiresAt == nil || !room.ExpiresAt.Equal(wantExpiresAt) {
		t.Fatalf("room expires_at = %v, want %v", room.ExpiresAt, wantExpiresAt)
	}
	if !room.UpdatedAt.Equal(leftAt) {
		t.Fatalf("room updated_at = %v, want %v", room.UpdatedAt, leftAt)
	}
}
