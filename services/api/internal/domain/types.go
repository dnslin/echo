package domain

import (
	"errors"
	"time"
)

var (
	ErrInviteCodeConflict = errors.New("invite code conflict")
	ErrRoomNotFound       = errors.New("room not found")
	ErrMemberNotFound     = errors.New("member not found")
	ErrRoomExpired        = errors.New("room expired")
	ErrRoomFull           = errors.New("room full")
)

type RoomState string

const (
	RoomStateActive  RoomState = "active"
	RoomStateExpired RoomState = "expired"
)

type MemberState string

const (
	MemberStateOnline       MemberState = "online"
	MemberStateReconnecting MemberState = "reconnecting"
	MemberStateDisconnected MemberState = "disconnected"
)

type VoiceMode string

const (
	VoiceModePushToTalk VoiceMode = "push_to_talk"
)

type Room struct {
	ID              string
	Name            string
	InviteCode      string
	LiveKitRoomName string
	HostAnonymousID string
	HostNickname    string
	HostAvatarID    string
	State           RoomState
	CreatedAt       time.Time
	LastEmptyAt     *time.Time
	ExpiresAt       *time.Time
	UpdatedAt       time.Time
}

type Member struct {
	ID              string
	RoomID          string
	AnonymousID     string
	Nickname        string
	AvatarID        string
	IsHost          bool
	State           MemberState
	Muted           bool
	Speaking        bool
	VoiceMode       VoiceMode
	JoinedAt        time.Time
	LiveKitIdentity string
}
