package room

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

func TestJoinSucceedsWithNormalizedInviteAndNonHostMemberInitialState(t *testing.T) {
	repository := newJoinFakeRepository(joinTestRoom("room_join", "K7M9Q2", domain.RoomStateActive, nil))
	service := newTestService(repository, &fakeInviteGenerator{})

	result, err := service.Join(JoinInput{
		InviteCode:  " k7-m9 q2 ",
		AnonymousID: " anon_local_456 ",
		Nickname:    " Alice ",
		AvatarID:    " avatar_08 ",
	})
	if err != nil {
		t.Fatalf("Join returned error: %v", err)
	}

	if repository.findInviteCodes[0] != "K7M9Q2" {
		t.Fatalf("lookup invite code = %q, want K7M9Q2", repository.findInviteCodes[0])
	}
	assertCapacityStates(t, repository.countedStates)
	if len(repository.createdMembers) != 1 {
		t.Fatalf("created members = %d, want 1", len(repository.createdMembers))
	}
	member := repository.createdMembers[0]
	if result.Room.ID != "room_join" || result.Room.InviteCode != "K7M9Q2" {
		t.Fatalf("result room = %#v, want existing room", result.Room)
	}
	if result.Member != member {
		t.Fatalf("result member = %#v, want created member %#v", result.Member, member)
	}
	if member.ID != "mem_test" || member.RoomID != "room_join" || member.IsHost {
		t.Fatalf("member identity/host = %#v, want non-host joined member in room", member)
	}
	if member.AnonymousID != "anon_local_456" || member.Nickname != "Alice" || member.AvatarID != "avatar_08" {
		t.Fatalf("member display fields = %#v, want trimmed join fields", member)
	}
	if member.State != domain.MemberStateOnline || member.Muted || member.Speaking || member.VoiceMode != domain.VoiceModePushToTalk {
		t.Fatalf("member voice state = %#v, want online unmuted not speaking push_to_talk", member)
	}
	if member.LiveKitIdentity != member.ID || member.JoinedAt != fixedNow {
		t.Fatalf("member livekit/time = %#v, want identity id and fixed join time", member)
	}
}

func TestJoinAllowsDuplicateNickname(t *testing.T) {
	repository := newJoinFakeRepository(joinTestRoom("room_duplicate", "DUP123", domain.RoomStateActive, nil))
	service := newTestService(repository, &fakeInviteGenerator{})

	_, err := service.Join(JoinInput{
		InviteCode:  "DUP123",
		AnonymousID: "anon_local_456",
		Nickname:    "Alice",
		AvatarID:    "avatar_08",
	})
	if err != nil {
		t.Fatalf("Join with duplicate nickname returned error: %v", err)
	}
	if len(repository.createdMembers) != 1 || repository.createdMembers[0].Nickname != "Alice" {
		t.Fatalf("created members = %#v, want one duplicate-nickname member", repository.createdMembers)
	}
}

func TestJoinValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		input     JoinInput
		wantCode  string
		wantError string
	}{
		{
			name:      "empty invite code",
			input:     JoinInput{InviteCode: " - \t ", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: "avatar_08"},
			wantCode:  "empty_invite_code",
			wantError: "请输入邀请码",
		},
		{
			name:      "invalid invite format",
			input:     JoinInput{InviteCode: "ABC12!", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: "avatar_08"},
			wantCode:  "invalid_invite_format",
			wantError: "邀请码应为 6 位字母或数字",
		},
		{
			name:      "invalid invite length",
			input:     JoinInput{InviteCode: "ABC12", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: "avatar_08"},
			wantCode:  "invalid_invite_format",
			wantError: "邀请码应为 6 位字母或数字",
		},
		{
			name:      "empty anonymous id",
			input:     JoinInput{InviteCode: "ABC123", AnonymousID: " ", Nickname: "Alice", AvatarID: "avatar_08"},
			wantCode:  "invalid_anonymous_id",
			wantError: "匿名身份不能为空",
		},
		{
			name:      "anonymous id too long",
			input:     JoinInput{InviteCode: "ABC123", AnonymousID: strings.Repeat("a", 129), Nickname: "Alice", AvatarID: "avatar_08"},
			wantCode:  "anonymous_id_too_long",
			wantError: "匿名身份最多 128 个字符",
		},
		{
			name:      "empty nickname",
			input:     JoinInput{InviteCode: "ABC123", AnonymousID: "anon_local_456", Nickname: " ", AvatarID: "avatar_08"},
			wantCode:  "invalid_nickname",
			wantError: "请输入昵称",
		},
		{
			name:      "nickname too long",
			input:     JoinInput{InviteCode: "ABC123", AnonymousID: "anon_local_456", Nickname: strings.Repeat("你", 17), AvatarID: "avatar_08"},
			wantCode:  "nickname_too_long",
			wantError: "昵称最多 16 个字符",
		},
		{
			name:      "empty avatar id",
			input:     JoinInput{InviteCode: "ABC123", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: " "},
			wantCode:  "invalid_avatar_id",
			wantError: "请选择头像",
		},
		{
			name:      "avatar id too long",
			input:     JoinInput{InviteCode: "ABC123", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: strings.Repeat("a", 65)},
			wantCode:  "avatar_id_too_long",
			wantError: "头像标识最多 64 个字符",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(newJoinFakeRepository(joinTestRoom("room_join", "ABC123", domain.RoomStateActive, nil)), &fakeInviteGenerator{})
			_, err := service.Join(tt.input)
			assertValidationError(t, err, tt.wantCode, tt.wantError)
		})
	}
}

func TestJoinReturnsInviteNotFound(t *testing.T) {
	repository := &joinFakeRepository{findErr: domain.ErrRoomNotFound}
	service := newTestService(repository, &fakeInviteGenerator{})

	_, err := service.Join(JoinInput{InviteCode: "ABC123", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: "avatar_08"})
	if !errors.Is(err, ErrInviteNotFound) {
		t.Fatalf("Join error = %v, want ErrInviteNotFound", err)
	}
	if len(repository.createdMembers) != 0 {
		t.Fatalf("created members = %d, want 0", len(repository.createdMembers))
	}
}

func TestJoinRejectsExpiredRoomState(t *testing.T) {
	repository := newJoinFakeRepository(joinTestRoom("room_expired", "EXP123", domain.RoomStateExpired, nil))
	service := newTestService(repository, &fakeInviteGenerator{})

	_, err := service.Join(JoinInput{InviteCode: "EXP123", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: "avatar_08"})
	if !errors.Is(err, ErrRoomExpired) {
		t.Fatalf("Join error = %v, want ErrRoomExpired", err)
	}
	if len(repository.createdMembers) != 0 {
		t.Fatalf("created members = %d, want 0", len(repository.createdMembers))
	}
}

func TestJoinRejectsRoomPastExpiryAndMarksItExpired(t *testing.T) {
	expiresAt := fixedNow
	repository := newJoinFakeRepository(joinTestRoom("room_expired_at", "EXP456", domain.RoomStateActive, &expiresAt))
	service := newTestService(repository, &fakeInviteGenerator{})

	_, err := service.Join(JoinInput{InviteCode: "EXP456", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: "avatar_08"})
	if !errors.Is(err, ErrRoomExpired) {
		t.Fatalf("Join error = %v, want ErrRoomExpired", err)
	}
	if len(repository.markedExpiredRooms) != 1 || repository.markedExpiredRooms[0] != "room_expired_at" {
		t.Fatalf("marked expired rooms = %#v, want room_expired_at", repository.markedExpiredRooms)
	}
	if len(repository.createdMembers) != 0 {
		t.Fatalf("created members = %d, want 0", len(repository.createdMembers))
	}
}

func TestJoinRejectsFullRoom(t *testing.T) {
	repository := newJoinFakeRepository(joinTestRoom("room_full", "FULL10", domain.RoomStateActive, nil))
	repository.memberCount = maxRoomMembers
	service := newTestService(repository, &fakeInviteGenerator{})

	_, err := service.Join(JoinInput{InviteCode: "FULL10", AnonymousID: "anon_local_456", Nickname: "Alice", AvatarID: "avatar_08"})
	if !errors.Is(err, ErrRoomFull) {
		t.Fatalf("Join error = %v, want ErrRoomFull", err)
	}
	if len(repository.createdMembers) != 0 {
		t.Fatalf("created members = %d, want 0", len(repository.createdMembers))
	}
}

func assertCapacityStates(t *testing.T, states []domain.MemberState) {
	t.Helper()
	if len(states) != 2 || states[0] != domain.MemberStateOnline || states[1] != domain.MemberStateReconnecting {
		t.Fatalf("capacity states = %#v, want online and reconnecting", states)
	}
}

func joinTestRoom(id string, inviteCode string, state domain.RoomState, expiresAt *time.Time) domain.Room {
	return domain.Room{
		ID:              id,
		Name:            "今晚开黑",
		InviteCode:      inviteCode,
		LiveKitRoomName: "lk_" + id,
		HostAnonymousID: "anon_local_123",
		HostNickname:    "Alice",
		HostAvatarID:    "avatar_07",
		State:           state,
		CreatedAt:       fixedNow.Add(-time.Hour),
		ExpiresAt:       expiresAt,
		UpdatedAt:       fixedNow.Add(-time.Hour),
	}
}

type joinFakeRepository struct {
	roomsByInvite       map[string]domain.Room
	findErr             error
	memberCount         int
	countErr            error
	createMemberErr     error
	markRoomExpiredErr  error
	findInviteCodes     []string
	countedStates       []domain.MemberState
	createdMembers      []domain.Member
	markedExpiredRooms  []string
	markedExpiredTimes  []time.Time
	createdRoomWithHost []createCall
}

func newJoinFakeRepository(room domain.Room) *joinFakeRepository {
	return &joinFakeRepository{roomsByInvite: map[string]domain.Room{room.InviteCode: room}, memberCount: 1}
}

func (f *joinFakeRepository) CreateRoomWithMember(_ context.Context, room domain.Room, member domain.Member) error {
	f.createdRoomWithHost = append(f.createdRoomWithHost, createCall{room: room, member: member})
	return nil
}

func (f *joinFakeRepository) FindRoomByInviteCode(_ context.Context, inviteCode string) (domain.Room, error) {
	f.findInviteCodes = append(f.findInviteCodes, inviteCode)
	if f.findErr != nil {
		return domain.Room{}, f.findErr
	}
	room, ok := f.roomsByInvite[inviteCode]
	if !ok {
		return domain.Room{}, domain.ErrRoomNotFound
	}
	return room, nil
}

func (f *joinFakeRepository) CountRoomMembersByStates(_ context.Context, _ string, states []domain.MemberState) (int, error) {
	f.countedStates = append([]domain.MemberState(nil), states...)
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.memberCount, nil
}

func (f *joinFakeRepository) CreateMember(_ context.Context, member domain.Member) error {
	if f.createMemberErr != nil {
		return f.createMemberErr
	}
	f.createdMembers = append(f.createdMembers, member)
	return nil
}

func (f *joinFakeRepository) MarkRoomExpired(_ context.Context, roomID string, updatedAt time.Time) error {
	if f.markRoomExpiredErr != nil {
		return f.markRoomExpiredErr
	}
	f.markedExpiredRooms = append(f.markedExpiredRooms, roomID)
	f.markedExpiredTimes = append(f.markedExpiredTimes, updatedAt)
	return nil
}
