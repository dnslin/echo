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
	"echo/services/api/internal/invite"
)

const (
	maxAnonymousIDRunes = 128
	maxNicknameRunes    = 16
	maxAvatarIDRunes    = 64
	maxRoomNameRunes    = 24
	maxRoomMembers      = 10
	inviteCodeLength    = 6
	maxInviteAttempts   = 5
	defaultRoomName     = "临时房间"
)

var (
	ErrInviteCodeRetriesExhausted = errors.New("invite code retries exhausted")
	ErrInviteNotFound             = errors.New("invite not found")
	ErrRoomExpired                = errors.New("room expired")
	ErrRoomFull                   = errors.New("room full")
)

type Repository interface {
	CreateRoomWithMember(ctx context.Context, room domain.Room, member domain.Member) error
}

type joinRepository interface {
	FindRoomByInviteCode(ctx context.Context, inviteCode string) (domain.Room, error)
	CountRoomMembersByStates(ctx context.Context, roomID string, states []domain.MemberState) (int, error)
	CreateMember(ctx context.Context, member domain.Member) error
	MarkRoomExpired(ctx context.Context, roomID string, updatedAt time.Time) error
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

type JoinInput struct {
	InviteCode  string
	AnonymousID string
	Nickname    string
	AvatarID    string
}

type JoinResult struct {
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

func (s *Service) Join(input JoinInput) (JoinResult, error) {
	return s.JoinContext(context.Background(), input)
}

func (s *Service) JoinContext(ctx context.Context, input JoinInput) (JoinResult, error) {
	normalized, err := validateJoinInput(input)
	if err != nil {
		return JoinResult{}, err
	}
	if s == nil || s.repository == nil {
		return JoinResult{}, errors.New("room service is not configured")
	}
	repository, ok := s.repository.(joinRepository)
	if !ok {
		return JoinResult{}, errors.New("room repository does not support joining")
	}

	foundRoom, err := repository.FindRoomByInviteCode(ctx, normalized.inviteCode)
	if err != nil {
		if errors.Is(err, domain.ErrRoomNotFound) {
			return JoinResult{}, ErrInviteNotFound
		}
		return JoinResult{}, err
	}

	now := s.now().UTC()
	if foundRoom.State == domain.RoomStateExpired {
		return JoinResult{}, ErrRoomExpired
	}
	if foundRoom.ExpiresAt != nil && !foundRoom.ExpiresAt.After(now) {
		if err := repository.MarkRoomExpired(ctx, foundRoom.ID, now); err != nil {
			return JoinResult{}, err
		}
		return JoinResult{}, ErrRoomExpired
	}

	memberCount, err := repository.CountRoomMembersByStates(ctx, foundRoom.ID, []domain.MemberState{domain.MemberStateOnline, domain.MemberStateReconnecting})
	if err != nil {
		return JoinResult{}, err
	}
	if memberCount >= maxRoomMembers {
		return JoinResult{}, ErrRoomFull
	}

	memberID, err := s.idGenerator("mem")
	if err != nil {
		return JoinResult{}, err
	}
	member := buildJoinMember(normalized, foundRoom.ID, memberID, now)
	if err := repository.CreateMember(ctx, member); err != nil {
		return JoinResult{}, err
	}
	return JoinResult{Room: foundRoom, Member: member}, nil
}

type normalizedCreateInput struct {
	anonymousID string
	nickname    string
	avatarID    string
	roomName    string
}

type normalizedJoinInput struct {
	inviteCode  string
	anonymousID string
	nickname    string
	avatarID    string
}

type normalizedIdentityInput struct {
	anonymousID string
	nickname    string
	avatarID    string
}

func validateCreateInput(input CreateInput) (normalizedCreateInput, error) {
	identity, err := validateIdentityInput(input.AnonymousID, input.Nickname, input.AvatarID)
	if err != nil {
		return normalizedCreateInput{}, err
	}
	normalized := normalizedCreateInput{
		anonymousID: identity.anonymousID,
		nickname:    identity.nickname,
		avatarID:    identity.avatarID,
		roomName:    strings.TrimSpace(input.RoomName),
	}
	if utf8.RuneCountInString(normalized.roomName) > maxRoomNameRunes {
		return normalized, &ValidationError{Code: "room_name_too_long", Message: "房间名称最多 24 个字符"}
	}
	if normalized.roomName == "" {
		normalized.roomName = defaultRoomName
	}
	return normalized, nil
}

func validateJoinInput(input JoinInput) (normalizedJoinInput, error) {
	inviteCode, err := invite.Normalize(input.InviteCode)
	if err != nil {
		if errors.Is(err, invite.ErrEmptyCode) {
			return normalizedJoinInput{}, &ValidationError{Code: "empty_invite_code", Message: "请输入邀请码"}
		}
		return normalizedJoinInput{}, &ValidationError{Code: "invalid_invite_format", Message: "邀请码应为 6 位字母或数字"}
	}
	identity, err := validateIdentityInput(input.AnonymousID, input.Nickname, input.AvatarID)
	if err != nil {
		return normalizedJoinInput{}, err
	}
	return normalizedJoinInput{
		inviteCode:  inviteCode,
		anonymousID: identity.anonymousID,
		nickname:    identity.nickname,
		avatarID:    identity.avatarID,
	}, nil
}

func validateIdentityInput(anonymousID string, nickname string, avatarID string) (normalizedIdentityInput, error) {
	normalized := normalizedIdentityInput{
		anonymousID: strings.TrimSpace(anonymousID),
		nickname:    strings.TrimSpace(nickname),
		avatarID:    strings.TrimSpace(avatarID),
	}
	if normalized.anonymousID == "" {
		return normalized, &ValidationError{Code: "invalid_anonymous_id", Message: "匿名身份不能为空"}
	}
	if utf8.RuneCountInString(normalized.anonymousID) > maxAnonymousIDRunes {
		return normalized, &ValidationError{Code: "anonymous_id_too_long", Message: "匿名身份最多 128 个字符"}
	}
	if normalized.avatarID == "" {
		return normalized, &ValidationError{Code: "invalid_avatar_id", Message: "请选择头像"}
	}
	if utf8.RuneCountInString(normalized.avatarID) > maxAvatarIDRunes {
		return normalized, &ValidationError{Code: "avatar_id_too_long", Message: "头像标识最多 64 个字符"}
	}
	if normalized.nickname == "" {
		return normalized, &ValidationError{Code: "invalid_nickname", Message: "请输入昵称"}
	}
	if utf8.RuneCountInString(normalized.nickname) > maxNicknameRunes {
		return normalized, &ValidationError{Code: "nickname_too_long", Message: "昵称最多 16 个字符"}
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

func buildJoinMember(input normalizedJoinInput, roomID string, memberID string, now time.Time) domain.Member {
	return domain.Member{
		ID:              memberID,
		RoomID:          roomID,
		AnonymousID:     input.anonymousID,
		Nickname:        input.nickname,
		AvatarID:        input.avatarID,
		IsHost:          false,
		State:           domain.MemberStateOnline,
		Muted:           false,
		Speaking:        false,
		VoiceMode:       domain.VoiceModePushToTalk,
		JoinedAt:        now,
		LiveKitIdentity: memberID,
	}
}

func generateID(prefix string) (string, error) {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(bytes)), nil
}
