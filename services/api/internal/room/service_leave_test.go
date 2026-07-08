package room

import (
	"context"
	"errors"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

func TestLeaveValidationErrors(t *testing.T) {
	tests := []struct {
		name      string
		input     LeaveInput
		wantCode  string
		wantError string
	}{
		{
			name:      "blank room id",
			input:     LeaveInput{RoomID: " ", MemberID: "mem_test"},
			wantCode:  "invalid_room_id",
			wantError: "房间标识不能为空",
		},
		{
			name:      "blank member id",
			input:     LeaveInput{RoomID: "room_test", MemberID: "\t"},
			wantCode:  "invalid_member_id",
			wantError: "成员标识不能为空",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &leaveFakeRepository{}
			service := newTestService(repository, &fakeInviteGenerator{})

			_, err := service.LeaveContext(context.Background(), tt.input)

			assertValidationError(t, err, tt.wantCode, tt.wantError)
			if repository.leaveCalls != 0 {
				t.Fatalf("leave repository calls = %d, want 0", repository.leaveCalls)
			}
		})
	}
}

func TestLeaveDelegatesLifecycleInputsToRepository(t *testing.T) {
	repository := &leaveFakeRepository{
		leaveRoom:   domain.Room{ID: "room_test", State: domain.RoomStateActive},
		leaveMember: domain.Member{ID: "mem_test", RoomID: "room_test", State: domain.MemberStateDisconnected},
	}
	service := newTestService(repository, &fakeInviteGenerator{})
	ctx := context.WithValue(context.Background(), testRoomContextKey{}, "leave-context")

	result, err := service.LeaveContext(ctx, LeaveInput{RoomID: " room_test ", MemberID: " mem_test "})
	if err != nil {
		t.Fatalf("LeaveContext returned error: %v", err)
	}

	if result.Room.ID != "room_test" || result.Member.ID != "mem_test" {
		t.Fatalf("leave result = %#v, want repository room/member", result)
	}
	if repository.contextValue != "leave-context" {
		t.Fatalf("leave context value = %v, want leave-context", repository.contextValue)
	}
	if repository.leaveRoomID != "room_test" || repository.leaveMemberID != "mem_test" {
		t.Fatalf("leave ids = %q/%q, want trimmed room_test/mem_test", repository.leaveRoomID, repository.leaveMemberID)
	}
	assertCapacityStates(t, repository.leaveActiveStates)
	if !repository.leaveAt.Equal(fixedNow) {
		t.Fatalf("leave time = %v, want %v", repository.leaveAt, fixedNow)
	}
	if repository.leaveRetention != 30*time.Minute {
		t.Fatalf("leave retention = %v, want 30m", repository.leaveRetention)
	}
}

func TestLeaveMapsRepositorySentinels(t *testing.T) {
	tests := []struct {
		name     string
		storeErr error
		wantErr  error
	}{
		{name: "missing room", storeErr: domain.ErrRoomNotFound, wantErr: ErrRoomNotFound},
		{name: "missing member", storeErr: domain.ErrMemberNotFound, wantErr: ErrMemberNotFound},
		{name: "expired room", storeErr: domain.ErrRoomExpired, wantErr: ErrRoomExpired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repository := &leaveFakeRepository{leaveErr: tt.storeErr}
			service := newTestService(repository, &fakeInviteGenerator{})

			_, err := service.LeaveContext(context.Background(), LeaveInput{RoomID: "room_test", MemberID: "mem_test"})

			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("LeaveContext error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestExpireEmptyRoomsContextUsesControlledTime(t *testing.T) {
	repository := &leaveFakeRepository{expireCount: 2}
	service := newTestService(repository, &fakeInviteGenerator{})
	ctx := context.WithValue(context.Background(), testRoomContextKey{}, "expire-context")

	expired, err := service.ExpireEmptyRoomsContext(ctx)
	if err != nil {
		t.Fatalf("ExpireEmptyRoomsContext returned error: %v", err)
	}

	if expired != 2 {
		t.Fatalf("expired count = %d, want 2", expired)
	}
	if repository.contextValue != "expire-context" {
		t.Fatalf("expire context value = %v, want expire-context", repository.contextValue)
	}
	if !repository.expireNow.Equal(fixedNow) {
		t.Fatalf("expire now = %v, want %v", repository.expireNow, fixedNow)
	}
	if repository.expireRetention != 30*time.Minute {
		t.Fatalf("expire retention = %v, want 30m", repository.expireRetention)
	}
}

type testRoomContextKey struct{}

type leaveFakeRepository struct {
	leaveCalls        int
	expireCalls       int
	contextValue      any
	leaveRoomID       string
	leaveMemberID     string
	leaveActiveStates []domain.MemberState
	leaveAt           time.Time
	leaveRetention    time.Duration
	leaveRoom         domain.Room
	leaveMember       domain.Member
	leaveErr          error
	expireNow         time.Time
	expireRetention   time.Duration
	expireCount       int
	expireErr         error
}

func (f *leaveFakeRepository) CreateRoomWithMember(context.Context, domain.Room, domain.Member) error {
	return nil
}

func (f *leaveFakeRepository) LeaveRoomMember(ctx context.Context, roomID string, memberID string, activeStates []domain.MemberState, leftAt time.Time, retention time.Duration) (domain.Room, domain.Member, error) {
	f.leaveCalls++
	f.contextValue = ctx.Value(testRoomContextKey{})
	f.leaveRoomID = roomID
	f.leaveMemberID = memberID
	f.leaveActiveStates = append([]domain.MemberState(nil), activeStates...)
	f.leaveAt = leftAt
	f.leaveRetention = retention
	if f.leaveErr != nil {
		return domain.Room{}, domain.Member{}, f.leaveErr
	}
	return f.leaveRoom, f.leaveMember, nil
}

func (f *leaveFakeRepository) ExpireEmptyRooms(ctx context.Context, now time.Time, retention time.Duration) (int, error) {
	f.expireCalls++
	f.contextValue = ctx.Value(testRoomContextKey{})
	f.expireNow = now
	f.expireRetention = retention
	if f.expireErr != nil {
		return 0, f.expireErr
	}
	return f.expireCount, nil
}
