package store

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"echo/services/api/internal/domain"
	"gorm.io/gorm"
)

func TestUpdateMemberMutePersistsMutedAndClearsSpeaking(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_mute", "MUT301", createdAt)
	member := testMember("mem_member_mute", room.ID, createdAt)
	member.Speaking = true
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	transition, err := repository.UpdateMemberMute(context.Background(), room.ID, member.ID, true)
	if err != nil {
		t.Fatalf("UpdateMemberMute returned error: %v", err)
	}
	if !transition.MutedChanged || !transition.SpeakingChanged || !transition.Member.Muted || transition.Member.Speaking {
		t.Fatalf("mute transition = %#v, want committed muted=true speaking=false with both changes", transition)
	}

	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if !found.Muted {
		t.Fatalf("found member muted = %v, want true", found.Muted)
	}
	if found.Speaking {
		t.Fatalf("found member speaking = %v, want false after mute", found.Speaking)
	}
}

func TestUpdateMemberMuteRejectsDisconnectedMember(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_mute_inactive", "MUT302", createdAt)
	member := testMember("mem_member_mute_inactive", room.ID, createdAt)
	member.State = domain.MemberStateDisconnected
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	_, err := repository.UpdateMemberMute(context.Background(), room.ID, member.ID, true)
	if !errors.Is(err, domain.ErrMemberNotActive) {
		t.Fatalf("UpdateMemberMute error = %v, want ErrMemberNotActive", err)
	}

	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if found.Muted {
		t.Fatalf("disconnected member muted = true, want false")
	}
}

func TestUpdateMemberMuteDoesNotOverwriteConcurrentDisconnectCommit(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_mute_race", "MUT303", createdAt)
	member := testMember("mem_member_mute_race", room.ID, createdAt)
	member.Speaking = true
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	commandResult := make(chan error, 1)
	initialWaitCount := sqlDB.Stats().WaitCount
	err = db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("BEGIN IMMEDIATE").Error; err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_ = conn.Exec("ROLLBACK").Error
			}
		}()
		tx := conn.Session(&gorm.Session{SkipDefaultTransaction: true})
		if err := tx.Model(&MemberModel{}).Where("room_id = ? AND id = ?", room.ID, member.ID).Updates(map[string]any{
			"state":    string(domain.MemberStateDisconnected),
			"speaking": false,
		}).Error; err != nil {
			return err
		}

		go func() {
			_, err := repository.UpdateMemberMute(context.Background(), room.ID, member.ID, true)
			commandResult <- err
		}()
		timeout := time.NewTimer(time.Second)
		defer timeout.Stop()
		for sqlDB.Stats().WaitCount == initialWaitCount {
			select {
			case <-timeout.C:
				return errors.New("mute transition did not wait for the concurrent lifecycle transaction")
			default:
				runtime.Gosched()
			}
		}
		if err := conn.Exec("COMMIT").Error; err != nil {
			return err
		}
		committed = true
		return nil
	})
	if err != nil {
		t.Fatalf("concurrent disconnect transaction returned error: %v", err)
	}
	if err := <-commandResult; !errors.Is(err, domain.ErrMemberNotActive) {
		t.Fatalf("UpdateMemberMute error = %v, want ErrMemberNotActive after committed disconnect", err)
	}
	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if found.State != domain.MemberStateDisconnected || found.Muted || found.Speaking {
		t.Fatalf("member after concurrent mute = %#v, want disconnected muted=false speaking=false", found)
	}
}

func TestUpdateMemberSpeakingPersistsSpeakingRoundTrip(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_speaking", "SPK301", createdAt)
	member := testMember("mem_member_speaking", room.ID, createdAt)
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	started, err := repository.UpdateMemberSpeaking(context.Background(), room.ID, member.ID, true)
	if err != nil {
		t.Fatalf("UpdateMemberSpeaking(true) returned error: %v", err)
	}
	if !started.Changed || !started.Member.Speaking {
		t.Fatalf("speaking true transition = %#v, want changed committed speaking=true", started)
	}
	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID after true returned error: %v", err)
	}
	if !found.Speaking {
		t.Fatalf("found member speaking after true = %v, want true", found.Speaking)
	}

	stopped, err := repository.UpdateMemberSpeaking(context.Background(), room.ID, member.ID, false)
	if err != nil {
		t.Fatalf("UpdateMemberSpeaking(false) returned error: %v", err)
	}
	if !stopped.Changed || stopped.Member.Speaking {
		t.Fatalf("speaking false transition = %#v, want changed committed speaking=false", stopped)
	}
	found, err = repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID after false returned error: %v", err)
	}
	if found.Speaking {
		t.Fatalf("found member speaking after false = %v, want false", found.Speaking)
	}
}

func TestUpdateMemberSpeakingRejectsDisconnectedMember(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_speaking_inactive", "SPK302", createdAt)
	member := testMember("mem_member_speaking_inactive", room.ID, createdAt)
	member.State = domain.MemberStateDisconnected
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	_, err := repository.UpdateMemberSpeaking(context.Background(), room.ID, member.ID, true)
	if !errors.Is(err, domain.ErrMemberNotActive) {
		t.Fatalf("UpdateMemberSpeaking error = %v, want ErrMemberNotActive", err)
	}

	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if found.Speaking {
		t.Fatalf("disconnected member speaking = true, want false")
	}
}

func TestUpdateMemberSpeakingRepairsMutedSpeakingInvariant(t *testing.T) {
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		roomID     string
		inviteCode string
		speaking   bool
	}{
		{name: "requested true", roomID: "room_speaking_repair_true", inviteCode: "SPKRT1", speaking: true},
		{name: "requested false", roomID: "room_speaking_repair_false", inviteCode: "SPKRF1", speaking: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := openTestSQLite(t)
			repository := NewRepository(db)
			room := testRoom(tt.roomID, tt.inviteCode, createdAt)
			member := testMember("mem_"+tt.roomID, room.ID, createdAt)
			member.Muted = true
			member.Speaking = true
			if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
				t.Fatalf("CreateRoomWithMember returned error: %v", err)
			}

			transition, err := repository.UpdateMemberSpeaking(context.Background(), room.ID, member.ID, tt.speaking)
			if err != nil {
				t.Fatalf("UpdateMemberSpeaking(%v) returned error: %v", tt.speaking, err)
			}
			if !transition.Changed || !transition.Member.Muted || transition.Member.Speaking {
				t.Fatalf("speaking repair transition = %#v, want changed muted=true speaking=false", transition)
			}
			found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
			if err != nil {
				t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
			}
			if !found.Muted || found.Speaking {
				t.Fatalf("repaired member muted/speaking = %v/%v, want true/false", found.Muted, found.Speaking)
			}
		})
	}
}

func TestUpdateMemberSpeakingDoesNotOverwriteConcurrentDisconnectCommit(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	room := testRoom("room_member_speaking_race", "SPK303", createdAt)
	member := testMember("mem_member_speaking_race", room.ID, createdAt)
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)

	commandResult := make(chan error, 1)
	initialWaitCount := sqlDB.Stats().WaitCount
	err = db.Connection(func(conn *gorm.DB) error {
		if err := conn.Exec("BEGIN IMMEDIATE").Error; err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				_ = conn.Exec("ROLLBACK").Error
			}
		}()
		tx := conn.Session(&gorm.Session{SkipDefaultTransaction: true})
		if err := tx.Model(&MemberModel{}).Where("room_id = ? AND id = ?", room.ID, member.ID).Updates(map[string]any{
			"state":    string(domain.MemberStateDisconnected),
			"speaking": false,
		}).Error; err != nil {
			return err
		}

		go func() {
			_, err := repository.UpdateMemberSpeaking(context.Background(), room.ID, member.ID, true)
			commandResult <- err
		}()
		timeout := time.NewTimer(time.Second)
		defer timeout.Stop()
		for sqlDB.Stats().WaitCount == initialWaitCount {
			select {
			case <-timeout.C:
				return errors.New("speaking transition did not wait for the concurrent lifecycle transaction")
			default:
				runtime.Gosched()
			}
		}
		if err := conn.Exec("COMMIT").Error; err != nil {
			return err
		}
		committed = true
		return nil
	})
	if err != nil {
		t.Fatalf("concurrent disconnect transaction returned error: %v", err)
	}
	if err := <-commandResult; !errors.Is(err, domain.ErrMemberNotActive) {
		t.Fatalf("UpdateMemberSpeaking error = %v, want ErrMemberNotActive after committed disconnect", err)
	}
	found, err := repository.FindMemberByRoomAndID(context.Background(), room.ID, member.ID)
	if err != nil {
		t.Fatalf("FindMemberByRoomAndID returned error: %v", err)
	}
	if found.State != domain.MemberStateDisconnected || found.Speaking {
		t.Fatalf("member after concurrent command = %#v, want disconnected and speaking=false", found)
	}
}

func TestDisconnectMemberReusesLeaveLifecycleSemantics(t *testing.T) {
	db := openTestSQLite(t)
	repository := NewRepository(db)
	createdAt := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	disconnectedAt := createdAt.Add(5 * time.Minute)
	room := testRoom("room_disconnect_member", "DSC301", createdAt)
	member := testMember("mem_disconnect_member", room.ID, createdAt)
	member.Speaking = true
	if err := repository.CreateRoomWithMember(context.Background(), room, member); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	transition, err := repository.DisconnectMember(context.Background(), room.ID, member.ID, activeMemberStates(), disconnectedAt, testEmptyRoomRetention)
	if err != nil {
		t.Fatalf("DisconnectMember returned error: %v", err)
	}
	if !transition.Transitioned {
		t.Fatalf("DisconnectMember Transitioned = false, want true")
	}
	disconnectedRoom := transition.Room
	disconnectedMember := transition.Member

	if disconnectedMember.State != domain.MemberStateDisconnected {
		t.Fatalf("disconnected member state = %q, want disconnected", disconnectedMember.State)
	}
	if disconnectedMember.Speaking {
		t.Fatalf("disconnected member speaking = %v, want false", disconnectedMember.Speaking)
	}
	assertRoomRetentionStarted(t, disconnectedRoom, disconnectedAt)
}
