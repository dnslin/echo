package store

import (
	"context"
	"testing"
	"time"

	"echo/services/api/internal/domain"
)

func TestListRoomMembersByStatesOrdersActiveMembersAndExcludesDisconnected(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	base := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_list", "LIST01", base)
	host := testMember("mem_host_list", room.ID, base)
	if err := repository.CreateRoomWithMember(context.Background(), room, host); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	members := []domain.Member{
		joinMember("mem_late", room.ID, domain.MemberStateOnline, base.Add(3*time.Minute)),
		joinMember("mem_tie_b", room.ID, domain.MemberStateReconnecting, base.Add(2*time.Minute)),
		joinMember("mem_tie_a", room.ID, domain.MemberStateOnline, base.Add(2*time.Minute)),
		joinMember("mem_disconnected", room.ID, domain.MemberStateDisconnected, base.Add(time.Minute)),
	}
	for _, member := range members {
		if err := repository.CreateMember(context.Background(), member); err != nil {
			t.Fatalf("CreateMember(%s) returned error: %v", member.ID, err)
		}
	}

	listed, err := repository.ListRoomMembersByStates(context.Background(), room.ID, activeMemberStates())
	if err != nil {
		t.Fatalf("ListRoomMembersByStates returned error: %v", err)
	}
	wantIDs := []string{"mem_host_list", "mem_tie_a", "mem_tie_b", "mem_late"}
	if len(listed) != len(wantIDs) {
		t.Fatalf("listed member count = %d, want %d: %#v", len(listed), len(wantIDs), listed)
	}
	for i, wantID := range wantIDs {
		if listed[i].ID != wantID {
			t.Fatalf("listed[%d].ID = %q, want %q; full list %#v", i, listed[i].ID, wantID, listed)
		}
		if listed[i].State == domain.MemberStateDisconnected {
			t.Fatalf("listed[%d] is disconnected: %#v", i, listed[i])
		}
	}
}

func TestListRoomMembersByStatesReturnsEmptyForEmptyStates(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)

	listed, err := repository.ListRoomMembersByStates(context.Background(), "room_any", nil)
	if err != nil {
		t.Fatalf("ListRoomMembersByStates returned error: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("listed member count = %d, want 0", len(listed))
	}
}
