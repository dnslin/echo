package room

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

var fixedNow = time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)

func TestCreateSucceedsWithHostMemberInitialState(t *testing.T) {
	repository := &fakeRepository{}
	service := newTestService(repository, &fakeInviteGenerator{codes: []string{"K7M9Q2"}})

	result, err := service.Create(CreateInput{
		AnonymousID: "anon_local_123",
		Nickname:    " Alice ",
		AvatarID:    "avatar_07",
		RoomName:    " 今晚开黑 ",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}

	if len(repository.creates) != 1 {
		t.Fatalf("repository create calls = %d, want 1", len(repository.creates))
	}
	room := result.Room
	member := result.Member
	if room.ID != "room_test" || room.Name != "今晚开黑" || room.InviteCode != "K7M9Q2" {
		t.Fatalf("room identity fields = %#v, want trimmed room with invite", room)
	}
	if room.State != domain.RoomStateActive || room.CreatedAt != fixedNow {
		t.Fatalf("room state/time = %q/%v, want active/%v", room.State, room.CreatedAt, fixedNow)
	}
	if room.LastEmptyAt != nil || room.ExpiresAt != nil {
		t.Fatalf("room empty/expiry fields = %v/%v, want nil/nil", room.LastEmptyAt, room.ExpiresAt)
	}
	if member.ID != "mem_test" || member.RoomID != room.ID || !member.IsHost {
		t.Fatalf("member identity/host fields = %#v, want host member in room", member)
	}
	if member.Nickname != "Alice" || member.AnonymousID != "anon_local_123" || member.AvatarID != "avatar_07" {
		t.Fatalf("member display fields = %#v, want normalized creator fields", member)
	}
	if member.State != domain.MemberStateOnline || member.Muted || member.Speaking || member.VoiceMode != domain.VoiceModePushToTalk {
		t.Fatalf("member voice state = %#v, want online unmuted not speaking push_to_talk", member)
	}
	if member.LiveKitIdentity != member.ID || member.JoinedAt != fixedNow {
		t.Fatalf("member livekit/time fields = %#v, want identity id and fixed join time", member)
	}
}

func TestCreateUsesDefaultRoomNameWhenBlank(t *testing.T) {
	repository := &fakeRepository{}
	service := newTestService(repository, &fakeInviteGenerator{codes: []string{"ABC123"}})

	result, err := service.Create(CreateInput{
		AnonymousID: "anon_local_123",
		Nickname:    "Alice",
		AvatarID:    "avatar_07",
		RoomName:    "   ",
	})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if result.Room.Name != defaultRoomName {
		t.Fatalf("default room name = %q, want %q", result.Room.Name, defaultRoomName)
	}
}

func TestCreateValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		input     CreateInput
		wantCode  string
		wantError string
	}{
		{
			name:      "empty anonymous id",
			input:     CreateInput{AnonymousID: " ", Nickname: "Alice", AvatarID: "avatar_07"},
			wantCode:  "invalid_anonymous_id",
			wantError: "匿名身份不能为空",
		},
		{
			name:      "empty avatar id",
			input:     CreateInput{AnonymousID: "anon_local_123", Nickname: "Alice", AvatarID: " "},
			wantCode:  "invalid_avatar_id",
			wantError: "请选择头像",
		},
		{
			name:      "empty nickname",
			input:     CreateInput{AnonymousID: "anon_local_123", Nickname: "   ", AvatarID: "avatar_07"},
			wantCode:  "invalid_nickname",
			wantError: "请输入昵称",
		},
		{
			name:      "nickname too long",
			input:     CreateInput{AnonymousID: "anon_local_123", Nickname: strings.Repeat("你", 17), AvatarID: "avatar_07"},
			wantCode:  "nickname_too_long",
			wantError: "昵称最多 16 个字符",
		},
		{
			name:      "room name too long",
			input:     CreateInput{AnonymousID: "anon_local_123", Nickname: "Alice", AvatarID: "avatar_07", RoomName: strings.Repeat("房", 25)},
			wantCode:  "room_name_too_long",
			wantError: "房间名称最多 24 个字符",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newTestService(&fakeRepository{}, &fakeInviteGenerator{codes: []string{"ABC123"}})
			_, err := service.Create(tt.input)
			assertValidationError(t, err, tt.wantCode, tt.wantError)
		})
	}
}

func TestCreateRetriesInviteCodeConflict(t *testing.T) {
	repository := &fakeRepository{errors: []error{domain.ErrInviteCodeConflict, nil}}
	service := newTestService(repository, &fakeInviteGenerator{codes: []string{"AAAAAA", "BBBBBB"}})

	result, err := service.Create(CreateInput{AnonymousID: "anon_local_123", Nickname: "Alice", AvatarID: "avatar_07"})
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if len(repository.creates) != 2 {
		t.Fatalf("repository create calls = %d, want 2", len(repository.creates))
	}
	if result.Room.InviteCode != "BBBBBB" {
		t.Fatalf("final invite code = %q, want BBBBBB", result.Room.InviteCode)
	}
}

func TestCreateReturnsErrorAfterInviteCodeRetriesExhausted(t *testing.T) {
	errorsToReturn := make([]error, maxInviteAttempts)
	codes := make([]string, maxInviteAttempts)
	for i := range errorsToReturn {
		errorsToReturn[i] = domain.ErrInviteCodeConflict
		codes[i] = "AAAAAA"
	}
	repository := &fakeRepository{errors: errorsToReturn}
	service := newTestService(repository, &fakeInviteGenerator{codes: codes})

	_, err := service.Create(CreateInput{AnonymousID: "anon_local_123", Nickname: "Alice", AvatarID: "avatar_07"})
	if !errors.Is(err, ErrInviteCodeRetriesExhausted) {
		t.Fatalf("Create error = %v, want ErrInviteCodeRetriesExhausted", err)
	}
}

func assertValidationError(t *testing.T, err error, wantCode string, wantMessage string) {
	t.Helper()
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error = %v, want ValidationError", err)
	}
	if validationErr.Code != wantCode || validationErr.Message != wantMessage {
		t.Fatalf("validation error = %s/%s, want %s/%s", validationErr.Code, validationErr.Message, wantCode, wantMessage)
	}
}

type createCall struct {
	room   domain.Room
	member domain.Member
}

type fakeRepository struct {
	creates []createCall
	errors  []error
}

func (f *fakeRepository) CreateRoomWithMember(_ context.Context, room domain.Room, member domain.Member) error {
	f.creates = append(f.creates, createCall{room: room, member: member})
	if len(f.errors) == 0 {
		return nil
	}
	err := f.errors[0]
	f.errors = f.errors[1:]
	return err
}

type fakeInviteGenerator struct {
	codes []string
}

func (f *fakeInviteGenerator) Generate(_ int) (string, error) {
	if len(f.codes) == 0 {
		return "ABC123", nil
	}
	code := f.codes[0]
	f.codes = f.codes[1:]
	return code, nil
}

func newTestService(repository Repository, generator InviteGenerator) *Service {
	service := NewService(repository, generator)
	service.now = func() time.Time { return fixedNow }
	service.idGenerator = func(prefix string) (string, error) { return prefix + "_test", nil }
	return service
}
