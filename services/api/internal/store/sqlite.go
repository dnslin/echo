package store

import (
	"context"
	"errors"
	"strings"
	"time"

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

func (r *Repository) FindRoomByInviteCode(ctx context.Context, inviteCode string) (domain.Room, error) {
	if r == nil || r.db == nil {
		return domain.Room{}, errors.New("store repository requires a database")
	}

	var model RoomModel
	if err := r.db.WithContext(ctx).First(&model, "invite_code = ?", inviteCode).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Room{}, domain.ErrRoomNotFound
		}
		return domain.Room{}, err
	}
	return modelToRoom(model), nil
}

func (r *Repository) CountRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) (int, error) {
	if r == nil || r.db == nil {
		return 0, errors.New("store repository requires a database")
	}
	if len(states) == 0 {
		return 0, nil
	}

	stateValues := make([]string, 0, len(states))
	for _, state := range states {
		stateValues = append(stateValues, string(state))
	}
	var count int64
	if err := r.db.WithContext(ctx).Model(&MemberModel{}).Where("room_id = ? AND state IN ?", roomID, stateValues).Count(&count).Error; err != nil {
		return 0, err
	}
	return int(count), nil
}

func (r *Repository) CreateMember(ctx context.Context, member domain.Member) error {
	if r == nil || r.db == nil {
		return errors.New("store repository requires a database")
	}
	return r.db.WithContext(ctx).Create(memberToModel(member)).Error
}

func (r *Repository) MarkRoomExpired(ctx context.Context, roomID string, updatedAt time.Time) error {
	if r == nil || r.db == nil {
		return errors.New("store repository requires a database")
	}
	result := r.db.WithContext(ctx).Model(&RoomModel{}).Where("id = ?", roomID).Updates(map[string]any{
		"state":      string(domain.RoomStateExpired),
		"updated_at": updatedAt,
	})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrRoomNotFound
	}
	return nil
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

func modelToRoom(model RoomModel) domain.Room {
	return domain.Room{
		ID:              model.ID,
		Name:            model.Name,
		InviteCode:      model.InviteCode,
		LiveKitRoomName: model.LiveKitRoomName,
		HostAnonymousID: model.HostAnonymousID,
		HostNickname:    model.HostNickname,
		HostAvatarID:    model.HostAvatarID,
		State:           domain.RoomState(model.State),
		CreatedAt:       model.CreatedAt,
		LastEmptyAt:     model.LastEmptyAt,
		ExpiresAt:       model.ExpiresAt,
		UpdatedAt:       model.UpdatedAt,
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
