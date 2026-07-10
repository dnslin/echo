package room

import (
	"context"
	"errors"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

func TestUpdateMemberMuteContextReturnsAuthoritativeState(t *testing.T) {
	repository := &stateFakeRepository{
		roomValue: domain.Room{ID: "room_state", State: domain.RoomStateActive},
		member: domain.Member{
			ID:        "mem_state",
			RoomID:    "room_state",
			State:     domain.MemberStateOnline,
			Muted:     false,
			Speaking:  true,
			VoiceMode: domain.VoiceModePushToTalk,
		},
		leaveRoom:   domain.Room{ID: "room_state", State: domain.RoomStateActive},
		leaveMember: domain.Member{ID: "mem_state", RoomID: "room_state", State: domain.MemberStateDisconnected},
	}
	service := newTestService(repository, &fakeInviteGenerator{})
	ctx := context.WithValue(context.Background(), testRoomContextKey{}, "state-context")

	result, err := service.UpdateMemberMuteContext(ctx, UpdateMemberMuteInput{RoomID: " room_state ", MemberID: " mem_state ", Muted: true})
	if err != nil {
		t.Fatalf("UpdateMemberMuteContext returned error: %v", err)
	}

	if repository.contextValue != "state-context" {
		t.Fatalf("repository context value = %v, want state-context", repository.contextValue)
	}
	if repository.findRoomID != "room_state" || repository.findMemberRoomID != "room_state" || repository.findMemberID != "mem_state" {
		t.Fatalf("repository lookup ids = %q/%q/%q, want trimmed room/member ids", repository.findRoomID, repository.findMemberRoomID, repository.findMemberID)
	}
	if repository.updateMuteCalls != 1 || repository.updateMuteRoomID != "room_state" || repository.updateMuteMemberID != "mem_state" || !repository.updateMuteMuted {
		t.Fatalf("mute update call = %#v, want one trimmed true update", repository)
	}
	if !result.MutedChanged || !result.SpeakingChanged {
		t.Fatalf("mute result changed flags = %v/%v, want true/true", result.MutedChanged, result.SpeakingChanged)
	}
	if !result.Member.Muted || result.Member.Speaking {
		t.Fatalf("updated member muted/speaking = %v/%v, want true/false", result.Member.Muted, result.Member.Speaking)
	}
}

func TestUpdateMemberSpeakingContextIgnoresSpeakingTrueForMutedMember(t *testing.T) {
	repository := &stateFakeRepository{
		roomValue: domain.Room{ID: "room_state", State: domain.RoomStateActive},
		member: domain.Member{
			ID:        "mem_state",
			RoomID:    "room_state",
			State:     domain.MemberStateOnline,
			Muted:     true,
			Speaking:  false,
			VoiceMode: domain.VoiceModePushToTalk,
		},
	}
	service := newTestService(repository, &fakeInviteGenerator{})

	result, err := service.UpdateMemberSpeakingContext(context.Background(), UpdateMemberSpeakingInput{RoomID: "room_state", MemberID: "mem_state", Speaking: true})
	if err != nil {
		t.Fatalf("UpdateMemberSpeakingContext returned error: %v", err)
	}

	if repository.updateSpeakingCalls != 0 {
		t.Fatalf("update speaking calls = %d, want 0", repository.updateSpeakingCalls)
	}
	if result.Changed {
		t.Fatalf("speaking result changed = %v, want false", result.Changed)
	}
	if result.Member.Speaking {
		t.Fatalf("result member speaking = %v, want false", result.Member.Speaking)
	}
}

func TestUpdateMemberStateContextRejectsDisconnectedMember(t *testing.T) {
	repository := &stateFakeRepository{
		roomValue: domain.Room{ID: "room_state", State: domain.RoomStateActive},
		member: domain.Member{
			ID:        "mem_state",
			RoomID:    "room_state",
			State:     domain.MemberStateDisconnected,
			Muted:     false,
			Speaking:  false,
			VoiceMode: domain.VoiceModePushToTalk,
		},
	}
	service := newTestService(repository, &fakeInviteGenerator{})

	_, err := service.UpdateMemberMuteContext(context.Background(), UpdateMemberMuteInput{RoomID: "room_state", MemberID: "mem_state", Muted: true})
	if !errors.Is(err, ErrMemberNotActive) {
		t.Fatalf("UpdateMemberMuteContext error = %v, want ErrMemberNotActive", err)
	}
	_, err = service.UpdateMemberSpeakingContext(context.Background(), UpdateMemberSpeakingInput{RoomID: "room_state", MemberID: "mem_state", Speaking: true})
	if !errors.Is(err, ErrMemberNotActive) {
		t.Fatalf("UpdateMemberSpeakingContext error = %v, want ErrMemberNotActive", err)
	}
	if repository.updateMuteCalls != 0 || repository.updateSpeakingCalls != 0 {
		t.Fatalf("update calls mute/speaking = %d/%d, want 0/0", repository.updateMuteCalls, repository.updateSpeakingCalls)
	}
}

func TestDisconnectMemberContextDelegatesLifecycleInputsToRepository(t *testing.T) {
	repository := &stateFakeRepository{
		roomValue:   domain.Room{ID: "room_state", State: domain.RoomStateActive},
		member:      domain.Member{ID: "mem_state", RoomID: "room_state", State: domain.MemberStateOnline},
		leaveRoom:   domain.Room{ID: "room_state", State: domain.RoomStateActive},
		leaveMember: domain.Member{ID: "mem_state", RoomID: "room_state", State: domain.MemberStateDisconnected},
	}
	service := newTestService(repository, &fakeInviteGenerator{})
	ctx := context.WithValue(context.Background(), testRoomContextKey{}, "disconnect-context")

	result, err := service.DisconnectMemberContext(ctx, DisconnectMemberInput{RoomID: " room_state ", MemberID: " mem_state "})
	if err != nil {
		t.Fatalf("DisconnectMemberContext returned error: %v", err)
	}

	if result.Room.ID != "room_state" || result.Member.ID != "mem_state" {
		t.Fatalf("disconnect result = %#v, want repository room/member", result)
	}
	if repository.contextValue != "disconnect-context" {
		t.Fatalf("repository context value = %v, want disconnect-context", repository.contextValue)
	}
	if repository.leaveRoomID != "room_state" || repository.leaveMemberID != "mem_state" {
		t.Fatalf("disconnect ids = %q/%q, want trimmed room/member ids", repository.leaveRoomID, repository.leaveMemberID)
	}
	assertCapacityStates(t, repository.leaveActiveStates)
	if !repository.leaveAt.Equal(fixedNow) {
		t.Fatalf("disconnect time = %v, want %v", repository.leaveAt, fixedNow)
	}
	if repository.leaveRetention != 30*time.Minute {
		t.Fatalf("disconnect retention = %v, want 30m", repository.leaveRetention)
	}
}

func TestDisconnectMemberContextMapsRepositorySentinels(t *testing.T) {
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
			repository := &stateFakeRepository{roomValue: domain.Room{ID: "room_state", State: domain.RoomStateActive}, member: domain.Member{ID: "mem_state", RoomID: "room_state", State: domain.MemberStateOnline}, leaveErr: tt.storeErr}
			service := newTestService(repository, &fakeInviteGenerator{})

			_, err := service.DisconnectMemberContext(context.Background(), DisconnectMemberInput{RoomID: "room_state", MemberID: "mem_state"})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("DisconnectMemberContext error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

type stateFakeRepository struct {
	roomValue            domain.Room
	member               domain.Member
	roomErr              error
	memberErr            error
	contextValue         any
	findRoomID           string
	findMemberRoomID     string
	findMemberID         string
	updateMuteCalls      int
	updateMuteRoomID     string
	updateMuteMemberID   string
	updateMuteMuted      bool
	updateMuteErr        error
	updateSpeakingCalls  int
	updateSpeakingRoomID string
	updateSpeakingID     string
	updateSpeakingValue  bool
	updateSpeakingErr    error
	leaveRoomID          string
	leaveMemberID        string
	leaveActiveStates    []domain.MemberState
	leaveAt              time.Time
	leaveRetention       time.Duration
	leaveRoom            domain.Room
	leaveMember          domain.Member
	leaveErr             error
}

func (f *stateFakeRepository) CreateRoomWithMember(context.Context, domain.Room, domain.Member) error {
	return nil
}

func (f *stateFakeRepository) FindRoomByID(ctx context.Context, roomID string) (domain.Room, error) {
	f.contextValue = ctx.Value(testRoomContextKey{})
	f.findRoomID = roomID
	if f.roomErr != nil {
		return domain.Room{}, f.roomErr
	}
	return f.roomValue, nil
}

func (f *stateFakeRepository) FindMemberByRoomAndID(_ context.Context, roomID string, memberID string) (domain.Member, error) {
	f.findMemberRoomID = roomID
	f.findMemberID = memberID
	if f.memberErr != nil {
		return domain.Member{}, f.memberErr
	}
	return f.member, nil
}

func (f *stateFakeRepository) UpdateMemberMute(_ context.Context, roomID string, memberID string, muted bool) error {
	f.updateMuteCalls++
	f.updateMuteRoomID = roomID
	f.updateMuteMemberID = memberID
	f.updateMuteMuted = muted
	if f.updateMuteErr != nil {
		return f.updateMuteErr
	}
	f.member.Muted = muted
	if muted {
		f.member.Speaking = false
	}
	return nil
}

func (f *stateFakeRepository) UpdateMemberSpeaking(_ context.Context, roomID string, memberID string, speaking bool) error {
	f.updateSpeakingCalls++
	f.updateSpeakingRoomID = roomID
	f.updateSpeakingID = memberID
	f.updateSpeakingValue = speaking
	if f.updateSpeakingErr != nil {
		return f.updateSpeakingErr
	}
	f.member.Speaking = speaking
	return nil
}

func (f *stateFakeRepository) LeaveRoomMember(ctx context.Context, roomID string, memberID string, activeStates []domain.MemberState, leftAt time.Time, retention time.Duration) (domain.Room, domain.Member, error) {
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
