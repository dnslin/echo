package store

import "time"

type RoomModel struct {
	ID              string     `gorm:"primaryKey;size:64"`
	Name            string     `gorm:"size:24;not null"`
	InviteCode      string     `gorm:"size:6;not null;uniqueIndex"`
	LiveKitRoomName string     `gorm:"column:livekit_room_name;size:128;not null"`
	HostAnonymousID string     `gorm:"size:128;not null"`
	HostNickname    string     `gorm:"size:16;not null"`
	HostAvatarID    string     `gorm:"size:64;not null"`
	State           string     `gorm:"size:16;not null;index"`
	CreatedAt       time.Time  `gorm:"not null"`
	LastEmptyAt     *time.Time `gorm:"default:null"`
	ExpiresAt       *time.Time `gorm:"default:null"`
	UpdatedAt       time.Time  `gorm:"not null"`
}

func (RoomModel) TableName() string {
	return "rooms"
}

type MemberModel struct {
	ID              string    `gorm:"primaryKey;size:64"`
	RoomID          string    `gorm:"size:64;not null;index"`
	AnonymousID     string    `gorm:"size:128;not null"`
	Nickname        string    `gorm:"size:16;not null"`
	AvatarID        string    `gorm:"size:64;not null"`
	IsHost          bool      `gorm:"not null"`
	State           string    `gorm:"size:16;not null;index"`
	Muted           bool      `gorm:"not null"`
	Speaking        bool      `gorm:"not null"`
	VoiceMode       string    `gorm:"size:32;not null"`
	JoinedAt        time.Time `gorm:"not null"`
	LiveKitIdentity string    `gorm:"column:livekit_identity;size:64;not null"`
}

func (MemberModel) TableName() string {
	return "members"
}
