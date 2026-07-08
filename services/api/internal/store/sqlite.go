package store

import (
	"context"
	"errors"
	"strings"

	"echo/services/api/internal/domain"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func OpenSQLite(path string) (*gorm.DB, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("sqlite path is required")
	}
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return nil, err
	}
	if err := db.AutoMigrate(&RoomModel{}, &MemberModel{}); err != nil {
		closeDB(db)
		return nil, err
	}
	return db, nil
}

func closeDB(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err == nil {
		_ = sqlDB.Close()
	}
}

type Repository struct {
	db *gorm.DB
}

func NewRepository(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CreateRoomWithMember(ctx context.Context, room domain.Room, member domain.Member) error {
	if r == nil || r.db == nil {
		return errors.New("store repository requires a database")
	}

	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(roomToModel(room)).Error; err != nil {
			if isInviteCodeConflict(err) {
				return domain.ErrInviteCodeConflict
			}
			return err
		}
		if err := tx.Create(memberToModel(member)).Error; err != nil {
			return err
		}
		return nil
	})
}

func roomToModel(room domain.Room) *RoomModel {
	return &RoomModel{
		ID:              room.ID,
		Name:            room.Name,
		InviteCode:      room.InviteCode,
		LiveKitRoomName: room.LiveKitRoomName,
		HostAnonymousID: room.HostAnonymousID,
		HostNickname:    room.HostNickname,
		HostAvatarID:    room.HostAvatarID,
		State:           string(room.State),
		CreatedAt:       room.CreatedAt,
		LastEmptyAt:     room.LastEmptyAt,
		ExpiresAt:       room.ExpiresAt,
		UpdatedAt:       room.UpdatedAt,
	}
}

func memberToModel(member domain.Member) *MemberModel {
	return &MemberModel{
		ID:              member.ID,
		RoomID:          member.RoomID,
		AnonymousID:     member.AnonymousID,
		Nickname:        member.Nickname,
		AvatarID:        member.AvatarID,
		IsHost:          member.IsHost,
		State:           string(member.State),
		Muted:           member.Muted,
		Speaking:        member.Speaking,
		VoiceMode:       string(member.VoiceMode),
		JoinedAt:        member.JoinedAt,
		LiveKitIdentity: member.LiveKitIdentity,
	}
}

func isInviteCodeConflict(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique") && strings.Contains(message, "invite")
}
