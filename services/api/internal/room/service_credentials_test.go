package room

import (
	"context"
	"errors"
	"testing"

	"echo/services/api/internal/domain"
)

func TestAuthorizeMemberContextAllowsActiveRoomMember(t *testing.T) {
	repository := newAuthorizeFakeRepository(
		domain.Room{ID: "room_auth", State: domain.RoomStateActive, LiveKitRoomName: "lk_room_auth"},
		domain.Member{ID: "mem_auth", RoomID: "room_auth", State: domain.MemberStateOnline, LiveKitIdentity: "mem_auth"},
	)
	service := newTestService(repository, &fakeInviteGenerator{})
	ctx := context.WithValue(context.Background(), testRoomContextKey{}, "authorize-context")

	result, err := service.AuthorizeMemberContext(ctx, AuthorizeMemberInput{RoomID: " room_auth ", MemberID: " mem_auth "})
	if err != nil {
		t.Fatalf("AuthorizeMemberContext returned error: %v", err)
	}
	if result.Room.ID != "room_auth" || result.Member.ID != "mem_auth" {
		t.Fatalf("authorize result = %#v, want active room/member", result)
	}
	if repository.contextValue != "authorize-context" {
		t.Fatalf("repository context value = %v, want authorize-context", repository.contextValue)
	}
	if repository.findRoomID != "room_auth" || repository.findMemberRoomID != "room_auth" || repository.findMemberID != "mem_auth" {
		t.Fatalf("repository lookup IDs = %q/%q/%q, want trimmed room/member IDs", repository.findRoomID, repository.findMemberRoomID, repository.findMemberID)
	}
}

func TestAuthorizeMemberContextAllowsReconnectingMember(t *testing.T) {
	repository := newAuthorizeFakeRepository(
		domain.Room{ID: "room_reconnect", State: domain.RoomStateActive, LiveKitRoomName: "lk_room_reconnect"},
		domain.Member{ID: "mem_reconnect", RoomID: "room_reconnect", State: domain.MemberStateReconnecting, LiveKitIdentity: "mem_reconnect"},
	)
	service := newTestService(repository, &fakeInviteGenerator{})

	result, err := service.AuthorizeMemberContext(context.Background(), AuthorizeMemberInput{RoomID: "room_reconnect", MemberID: "mem_reconnect"})
	if err != nil {
		t.Fatalf("AuthorizeMemberContext returned error: %v", err)
	}
	if result.Member.State != domain.MemberStateReconnecting {
		t.Fatalf("authorized member state = %q, want reconnecting", result.Member.State)
	}
}

func TestAuthorizeMemberContextRejectsInactiveOrMissingProductState(t *testing.T) {
	tests := []struct {
		name      string
		roomValue domain.Room
		member    domain.Member
		roomErr   error
		memberErr error
		wantErr   error
	}{
		{
			name:      "missing room",
			roomValue: domain.Room{ID: "room_missing", State: domain.RoomStateActive},
			member:    domain.Member{ID: "mem_missing", RoomID: "room_missing", State: domain.MemberStateOnline},
			roomErr:   domain.ErrRoomNotFound,
			wantErr:   ErrRoomNotFound,
		},
		{
			name:      "expired room",
			roomValue: domain.Room{ID: "room_expired", State: domain.RoomStateExpired},
			member:    domain.Member{ID: "mem_expired", RoomID: "room_expired", State: domain.MemberStateOnline},
			wantErr:   ErrRoomExpired,
		},
		{
			name:      "missing member",
			roomValue: domain.Room{ID: "room_missing_member", State: domain.RoomStateActive},
			member:    domain.Member{ID: "mem_missing_member", RoomID: "room_missing_member", State: domain.MemberStateOnline},
			memberErr: domain.ErrMemberNotFound,
			wantErr:   ErrMemberNotFound,
		},
		{
			name:      "wrong room member",
			roomValue: domain.Room{ID: "room_owner", State: domain.RoomStateActive},
			member:    domain.Member{ID: "mem_other_room", RoomID: "room_other", State: domain.MemberStateOnline},
			wantErr:   ErrMemberNotFound,
		},
		{
			name:      "disconnected member",
			roomValue: domain.Room{ID: "room_inactive", State: domain.RoomStateActive},
			member:    domain.Member{ID: "mem_inactive", RoomID: "room_inactive", State: domain.MemberStateDisconnected},
			wantErr:   ErrMemberNotActive,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := newAuthorizeFakeRepository(tt.roomValue, tt.member)
			repository.roomErr = tt.roomErr
			repository.memberErr = tt.memberErr
			service := newTestService(repository, &fakeInviteGenerator{})

			_, err := service.AuthorizeMemberContext(context.Background(), AuthorizeMemberInput{RoomID: tt.roomValue.ID, MemberID: tt.member.ID})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("AuthorizeMemberContext error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

type authorizeFakeRepository struct {
	roomValue        domain.Room
	member           domain.Member
	roomErr          error
	memberErr        error
	contextValue     any
	findRoomID       string
	findMemberRoomID string
	findMemberID     string
}

func newAuthorizeFakeRepository(roomValue domain.Room, member domain.Member) *authorizeFakeRepository {
	return &authorizeFakeRepository{roomValue: roomValue, member: member}
}

func (f *authorizeFakeRepository) CreateRoomWithMember(context.Context, domain.Room, domain.Member) error {
	return nil
}

func (f *authorizeFakeRepository) FindRoomByID(ctx context.Context, roomID string) (domain.Room, error) {
	f.contextValue = ctx.Value(testRoomContextKey{})
	f.findRoomID = roomID
	if f.roomErr != nil {
		return domain.Room{}, f.roomErr
	}
	return f.roomValue, nil
}

func (f *authorizeFakeRepository) FindMemberByRoomAndID(_ context.Context, roomID string, memberID string) (domain.Member, error) {
	f.findMemberRoomID = roomID
	f.findMemberID = memberID
	if f.memberErr != nil {
		return domain.Member{}, f.memberErr
	}
	return f.member, nil
}
