package room

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"echo/services/api/internal/domain"
)

const (
	maxNicknameRunes  = 16
	maxRoomNameRunes  = 24
	inviteCodeLength  = 6
	maxInviteAttempts = 5
	defaultRoomName   = "临时房间"
)

var ErrInviteCodeRetriesExhausted = errors.New("invite code retries exhausted")

type Repository interface {
	CreateRoomWithMember(ctx context.Context, room domain.Room, member domain.Member) error
}

type InviteGenerator interface {
	Generate(length int) (string, error)
}

type Service struct {
	repository       Repository
	inviteGenerator  InviteGenerator
	now              func() time.Time
	idGenerator      func(prefix string) (string, error)
	inviteLength     int
	maxInviteRetries int
}

type CreateInput struct {
	AnonymousID string
	Nickname    string
	AvatarID    string
	RoomName    string
}

type CreateResult struct {
	Room   domain.Room
	Member domain.Member
}

type ValidationError struct {
	Code    string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Code + ": " + e.Message
}

func NewService(repository Repository, inviteGenerator InviteGenerator) *Service {
	return &Service{
		repository:       repository,
		inviteGenerator:  inviteGenerator,
		now:              func() time.Time { return time.Now().UTC() },
		idGenerator:      generateID,
		inviteLength:     inviteCodeLength,
		maxInviteRetries: maxInviteAttempts,
	}
}

func (s *Service) Create(input CreateInput) (CreateResult, error) {
	return s.CreateContext(context.Background(), input)
}

func (s *Service) CreateContext(ctx context.Context, input CreateInput) (CreateResult, error) {
	normalized, err := validateCreateInput(input)
	if err != nil {
		return CreateResult{}, err
	}
	if s == nil || s.repository == nil || s.inviteGenerator == nil {
		return CreateResult{}, errors.New("room service is not configured")
	}

	roomID, err := s.idGenerator("room")
	if err != nil {
		return CreateResult{}, err
	}
	memberID, err := s.idGenerator("mem")
	if err != nil {
		return CreateResult{}, err
	}
	createdAt := s.now().UTC()

	for attempt := 0; attempt < s.maxInviteRetries; attempt++ {
		inviteCode, err := s.inviteGenerator.Generate(s.inviteLength)
		if err != nil {
			return CreateResult{}, err
		}
		result := buildCreateResult(normalized, roomID, memberID, inviteCode, createdAt)
		err = s.repository.CreateRoomWithMember(ctx, result.Room, result.Member)
		if err == nil {
			return result, nil
		}
		if errors.Is(err, domain.ErrInviteCodeConflict) {
			continue
		}
		return CreateResult{}, err
	}

	return CreateResult{}, ErrInviteCodeRetriesExhausted
}

type normalizedCreateInput struct {
	anonymousID string
	nickname    string
	avatarID    string
	roomName    string
}

func validateCreateInput(input CreateInput) (normalizedCreateInput, error) {
	normalized := normalizedCreateInput{
		anonymousID: strings.TrimSpace(input.AnonymousID),
		nickname:    strings.TrimSpace(input.Nickname),
		avatarID:    strings.TrimSpace(input.AvatarID),
		roomName:    strings.TrimSpace(input.RoomName),
	}

	if normalized.anonymousID == "" {
		return normalized, &ValidationError{Code: "invalid_anonymous_id", Message: "匿名身份不能为空"}
	}
	if normalized.avatarID == "" {
		return normalized, &ValidationError{Code: "invalid_avatar_id", Message: "请选择头像"}
	}
	if normalized.nickname == "" {
		return normalized, &ValidationError{Code: "invalid_nickname", Message: "请输入昵称"}
	}
	if utf8.RuneCountInString(normalized.nickname) > maxNicknameRunes {
		return normalized, &ValidationError{Code: "nickname_too_long", Message: "昵称最多 16 个字符"}
	}
	if utf8.RuneCountInString(normalized.roomName) > maxRoomNameRunes {
		return normalized, &ValidationError{Code: "room_name_too_long", Message: "房间名称最多 24 个字符"}
	}
	if normalized.roomName == "" {
		normalized.roomName = defaultRoomName
	}
	return normalized, nil
}

func buildCreateResult(input normalizedCreateInput, roomID string, memberID string, inviteCode string, now time.Time) CreateResult {
	room := domain.Room{
		ID:              roomID,
		Name:            input.roomName,
		InviteCode:      inviteCode,
		LiveKitRoomName: "lk_" + roomID,
		HostAnonymousID: input.anonymousID,
		HostNickname:    input.nickname,
		HostAvatarID:    input.avatarID,
		State:           domain.RoomStateActive,
		CreatedAt:       now,
		LastEmptyAt:     nil,
		ExpiresAt:       nil,
		UpdatedAt:       now,
	}
	member := domain.Member{
		ID:              memberID,
		RoomID:          roomID,
		AnonymousID:     input.anonymousID,
		Nickname:        input.nickname,
		AvatarID:        input.avatarID,
		IsHost:          true,
		State:           domain.MemberStateOnline,
		Muted:           false,
		Speaking:        false,
		VoiceMode:       domain.VoiceModePushToTalk,
		JoinedAt:        now,
		LiveKitIdentity: memberID,
	}
	return CreateResult{Room: room, Member: member}
}

func generateID(prefix string) (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(bytes)), nil
}
