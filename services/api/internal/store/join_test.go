package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

func TestFindRoomByInviteCodeReturnsPersistedRoom(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_lookup", "K7M9Q2", now)
	member := testMember("mem_lookup", room.ID, now)
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	found, err := repository.FindRoomByInviteCode(context.Background(), "K7M9Q2")
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}

	if found.ID != room.ID || found.InviteCode != room.InviteCode || found.State != domain.RoomStateActive {
		t.Fatalf("found room = %#v, want persisted active room %#v", found, room)
	}
}

func TestFindRoomByInviteCodeReturnsRoomNotFound(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)

	_, err := repository.FindRoomByInviteCode(context.Background(), "MISSING")
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("FindRoomByInviteCode missing error = %v, want ErrRoomNotFound", err)
	}
}

func TestCountRoomMembersByStatesCountsOnlineAndReconnectingOnly(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_count", "COUNT1", now)
	if err := repository.CreateRoomWithMember(context.Background(), room, testMember("mem_host", room.ID, now)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	members := []domain.Member{
		joinMember("mem_online", room.ID, domain.MemberStateOnline, now),
		joinMember("mem_reconnecting", room.ID, domain.MemberStateReconnecting, now),
		joinMember("mem_disconnected", room.ID, domain.MemberStateDisconnected, now),
	}
	for _, member := range members {
		if err := repository.CreateMember(context.Background(), member); err != nil {
			t.Fatalf("CreateMember(%s) returned error: %v", member.ID, err)
		}
	}

	count, err := repository.CountRoomMembersByStates(context.Background(), room.ID, []domain.MemberState{domain.MemberStateOnline, domain.MemberStateReconnecting})
	if err != nil {
		t.Fatalf("CountRoomMembersByStates returned error: %v", err)
	}
	if count != 3 {
		t.Fatalf("online/reconnecting count = %d, want 3", count)
	}
}

func TestCountRoomMembersByStatesReturnsZeroForEmptyStates(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)

	count, err := repository.CountRoomMembersByStates(context.Background(), "room_any", []domain.MemberState{})
	if err != nil {
		t.Fatalf("CountRoomMembersByStates returned error: %v", err)
	}
	if count != 0 {
		t.Fatalf("empty state count = %d, want 0", count)
	}
}

func TestCreateMemberPersistsNonHostDuplicateNickname(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_join", "JOIN01", now)
	if err := repository.CreateRoomWithMember(context.Background(), room, testMember("mem_host_join", room.ID, now)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	member := joinMember("mem_join", room.ID, domain.MemberStateOnline, now)
	member.Nickname = room.HostNickname

	if err := repository.CreateMember(context.Background(), member); err != nil {
		t.Fatalf("CreateMember returned error: %v", err)
	}

	var stored MemberModel
	if err := db.First(&stored, "id = ?", member.ID).Error; err != nil {
		t.Fatalf("joined member not found: %v", err)
	}
	if stored.IsHost || stored.Nickname != room.HostNickname || stored.State != string(domain.MemberStateOnline) {
		t.Fatalf("stored joined member = %#v, want non-host duplicate nickname online member", stored)
	}
	if stored.Muted || stored.Speaking || stored.VoiceMode != string(domain.VoiceModePushToTalk) || stored.LiveKitIdentity != member.ID {
		t.Fatalf("stored joined member voice fields = %#v, want unmuted not speaking push_to_talk identity %q", stored, member.ID)
	}
}

func TestJoinRoomWithMemberClearsRetainedEmptyRoomExpiryFields(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	lastEmptyAt := now.Add(-10 * time.Minute)
	expiresAt := now.Add(20 * time.Minute)
	room := testRoom("room_retained", "KEEP30", now.Add(-time.Hour))
	room.LastEmptyAt = &lastEmptyAt
	room.ExpiresAt = &expiresAt
	room.UpdatedAt = lastEmptyAt
	host := testMember("mem_retained_host", room.ID, now.Add(-time.Hour))
	host.State = domain.MemberStateDisconnected
	if err := repository.CreateRoomWithMember(context.Background(), room, host); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	member := joinMember("mem_retained_join", room.ID, domain.MemberStateOnline, now)
	joinedRoom, err := repository.JoinRoomWithMember(context.Background(), room, member, activeMemberStates(), 10, now)
	if err != nil {
		t.Fatalf("JoinRoomWithMember returned error: %v", err)
	}
	if joinedRoom.LastEmptyAt != nil || joinedRoom.ExpiresAt != nil {
		t.Fatalf("joined room empty/expiry fields = %v/%v, want nil/nil", joinedRoom.LastEmptyAt, joinedRoom.ExpiresAt)
	}
	if !joinedRoom.UpdatedAt.Equal(now) {
		t.Fatalf("joined room UpdatedAt = %v, want %v", joinedRoom.UpdatedAt, now)
	}

	found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}
	if found.LastEmptyAt != nil || found.ExpiresAt != nil {
		t.Fatalf("persisted room empty/expiry fields = %v/%v, want nil/nil", found.LastEmptyAt, found.ExpiresAt)
	}
}

func TestJoinRoomWithMemberConcurrentRequestsDoNotExceedCapacity(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_concurrent", "RACE10", now)
	if err := repository.CreateRoomWithMember(context.Background(), room, testMember("mem_host_race", room.ID, now)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	for i := 0; i < 8; i++ {
		member := joinMember("mem_existing_"+string(rune('a'+i)), room.ID, domain.MemberStateOnline, now)
		if err := repository.CreateMember(context.Background(), member); err != nil {
			t.Fatalf("CreateMember(%s) returned error: %v", member.ID, err)
		}
	}

	start := make(chan struct{})
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			member := joinMember("mem_race_join_"+string(rune('a'+index)), room.ID, domain.MemberStateOnline, now)
			_, errs[index] = repository.JoinRoomWithMember(context.Background(), room, member, activeMemberStates(), 10, now)
		}(i)
	}
	close(start)
	wg.Wait()

	successes := 0
	fullErrors := 0
	for _, err := range errs {
		if err == nil {
			successes++
			continue
		}
		if errors.Is(err, domain.ErrRoomFull) {
			fullErrors++
			continue
		}
		t.Fatalf("JoinRoomWithMember error = %v, want nil or ErrRoomFull", err)
	}
	if successes != 1 || fullErrors != 1 {
		t.Fatalf("join results = %d successes/%d full errors, want 1/1; errs=%#v", successes, fullErrors, errs)
	}
	count, err := repository.CountRoomMembersByStates(context.Background(), room.ID, activeMemberStates())
	if err != nil {
		t.Fatalf("CountRoomMembersByStates returned error: %v", err)
	}
	if count != 10 {
		t.Fatalf("active member count = %d, want 10", count)
	}
}

func TestMarkRoomExpiredUpdatesStateAndTimestamp(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	updatedAt := now.Add(time.Minute)
	room := testRoom("room_expire", "EXP123", now)
	if err := repository.CreateRoomWithMember(context.Background(), room, testMember("mem_expire", room.ID, now)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	if err := repository.MarkRoomExpired(context.Background(), room.ID, updatedAt); err != nil {
		t.Fatalf("MarkRoomExpired returned error: %v", err)
	}

	found, err := repository.FindRoomByInviteCode(context.Background(), room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode returned error: %v", err)
	}
	if found.State != domain.RoomStateExpired || !found.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("expired room state/time = %q/%v, want expired/%v", found.State, found.UpdatedAt, updatedAt)
	}
}

func TestMarkRoomExpiredReturnsRoomNotFound(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)

	err := repository.MarkRoomExpired(context.Background(), "room_missing", time.Date(2026, 7, 8, 12, 1, 0, 0, time.UTC))
	if !errors.Is(err, domain.ErrRoomNotFound) {
		t.Fatalf("MarkRoomExpired missing error = %v, want ErrRoomNotFound", err)
	}
}

func activeMemberStates() []domain.MemberState {
	return []domain.MemberState{domain.MemberStateOnline, domain.MemberStateReconnecting}
}

func joinMember(id string, roomID string, state domain.MemberState, now time.Time) domain.Member {
	return domain.Member{
		ID:              id,
		RoomID:          roomID,
		AnonymousID:     id + "_anon",
		Nickname:        "Alice",
		AvatarID:        "avatar_08",
		IsHost:          false,
		State:           state,
		Muted:           false,
		Speaking:        false,
		VoiceMode:       domain.VoiceModePushToTalk,
		JoinedAt:        now,
		LiveKitIdentity: id,
	}
}
