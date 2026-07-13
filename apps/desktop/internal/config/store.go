package config

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	DefaultPushToTalkKey  = "V"
	VoiceModePushToTalk   = "push_to_talk"
	VoiceModeFreeTalk     = "free_talk"
	MinimumOutputVolume   = 0
	MaximumOutputVolume   = 100
	DefaultOutputVolume   = MaximumOutputVolume
	MaximumNicknameLength = 16
)

var ErrInvalidNickname = errors.New("nickname must be 1 to 16 characters")

// Settings is the complete local preference contract persisted by the desktop shell.
type Settings struct {
	AnonymousID      string `json:"anonymous_id"`
	Nickname         string `json:"nickname"`
	AvatarID         string `json:"avatar_id"`
	PushToTalkKey    string `json:"push_to_talk_key"`
	MicrophoneDevice string `json:"microphone_device"`
	OutputDevice     string `json:"output_device"`
	VoiceMode        string `json:"voice_mode"`
	OutputVolume     int    `json:"output_volume"`
}

// Store owns one local settings file.
type Store struct {
	path string
}

// NewStore creates a Store for the supplied settings file path.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// NewDefaultStore creates a Store in the current user's echo configuration directory.
func NewDefaultStore() (*Store, error) {
	configDirectory, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get user config directory: %w", err)
	}

	return NewStore(filepath.Join(configDirectory, "echo", "settings.json")), nil
}

// Load returns usable settings, recovering missing or corrupt files by persisting safe defaults.
func (s *Store) Load() (Settings, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return Settings{}, fmt.Errorf("read settings: %w", err)
		}
		return s.createAndSaveDefaults()
	}

	settings, changed, err := decodeSettings(data)
	if err != nil {
		return s.createAndSaveDefaults()
	}
	if !changed {
		return settings, nil
	}
	if err := s.write(settings); err != nil {
		return Settings{}, err
	}

	return settings, nil
}

// Save validates and normalizes settings before replacing the persisted settings file.
func (s *Store) Save(settings Settings) error {
	nickname, err := validateNickname(settings.Nickname)
	if err != nil {
		return err
	}
	settings.Nickname = nickname

	normalized, _, err := normalize(settings)
	if err != nil {
		return err
	}

	return s.write(normalized)
}

// ResetAvatar generates and persists a new avatar without changing other settings.
func (s *Store) ResetAvatar() (Settings, error) {
	settings, err := s.Load()
	if err != nil {
		return Settings{}, err
	}

	avatarID, err := randomID()
	if err != nil {
		return Settings{}, fmt.Errorf("generate avatar ID: %w", err)
	}
	settings.AvatarID = avatarID
	if err := s.write(settings); err != nil {
		return Settings{}, err
	}

	return settings, nil
}

type settingsDocument struct {
	AnonymousID      string `json:"anonymous_id"`
	Nickname         string `json:"nickname"`
	AvatarID         string `json:"avatar_id"`
	PushToTalkKey    string `json:"push_to_talk_key"`
	MicrophoneDevice string `json:"microphone_device"`
	OutputDevice     string `json:"output_device"`
	VoiceMode        string `json:"voice_mode"`
	OutputVolume     *int   `json:"output_volume"`
}

func (s *Store) createAndSaveDefaults() (Settings, error) {
	settings, err := newDefaultSettings()
	if err != nil {
		return Settings{}, err
	}
	if err := s.write(settings); err != nil {
		return Settings{}, err
	}

	return settings, nil
}

func decodeSettings(data []byte) (Settings, bool, error) {
	var document settingsDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return Settings{}, false, err
	}

	settings := Settings{
		AnonymousID:      document.AnonymousID,
		Nickname:         document.Nickname,
		AvatarID:         document.AvatarID,
		PushToTalkKey:    document.PushToTalkKey,
		MicrophoneDevice: document.MicrophoneDevice,
		OutputDevice:     document.OutputDevice,
		VoiceMode:        document.VoiceMode,
	}
	changed := document.OutputVolume == nil
	if document.OutputVolume == nil {
		settings.OutputVolume = DefaultOutputVolume
	} else {
		settings.OutputVolume = *document.OutputVolume
	}

	normalized, normalizedChanged, err := normalize(settings)
	if err != nil {
		return Settings{}, false, err
	}

	return normalized, changed || normalizedChanged, nil
}

func newDefaultSettings() (Settings, error) {
	anonymousID, err := randomID()
	if err != nil {
		return Settings{}, fmt.Errorf("generate anonymous ID: %w", err)
	}
	avatarID, err := randomID()
	if err != nil {
		return Settings{}, fmt.Errorf("generate avatar ID: %w", err)
	}

	return Settings{
		AnonymousID:   anonymousID,
		AvatarID:      avatarID,
		PushToTalkKey: DefaultPushToTalkKey,
		VoiceMode:     VoiceModePushToTalk,
		OutputVolume:  DefaultOutputVolume,
	}, nil
}

func validateNickname(nickname string) (string, error) {
	nickname = strings.TrimSpace(nickname)
	if nickname == "" || utf8.RuneCountInString(nickname) > MaximumNicknameLength {
		return "", ErrInvalidNickname
	}

	return nickname, nil
}

func normalize(settings Settings) (Settings, bool, error) {
	changed := false
	nickname := strings.TrimSpace(settings.Nickname)
	if utf8.RuneCountInString(nickname) > MaximumNicknameLength {
		nickname = ""
	}
	if settings.Nickname != nickname {
		settings.Nickname = nickname
		changed = true
	}

	if strings.TrimSpace(settings.AnonymousID) == "" {
		anonymousID, err := randomID()
		if err != nil {
			return Settings{}, false, fmt.Errorf("generate anonymous ID: %w", err)
		}
		settings.AnonymousID = anonymousID
		changed = true
	}
	if strings.TrimSpace(settings.AvatarID) == "" {
		avatarID, err := randomID()
		if err != nil {
			return Settings{}, false, fmt.Errorf("generate avatar ID: %w", err)
		}
		settings.AvatarID = avatarID
		changed = true
	}
	if strings.TrimSpace(settings.PushToTalkKey) == "" {
		settings.PushToTalkKey = DefaultPushToTalkKey
		changed = true
	}
	if settings.VoiceMode != VoiceModePushToTalk && settings.VoiceMode != VoiceModeFreeTalk {
		settings.VoiceMode = VoiceModePushToTalk
		changed = true
	}
	if settings.OutputVolume < MinimumOutputVolume {
		settings.OutputVolume = MinimumOutputVolume
		changed = true
	}
	if settings.OutputVolume > MaximumOutputVolume {
		settings.OutputVolume = MaximumOutputVolume
		changed = true
	}

	return settings, changed, nil
}

func (s *Store) write(settings Settings) error {
	directory := filepath.Dir(s.path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	temporaryFile, err := os.CreateTemp(directory, ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary settings file: %w", err)
	}
	temporaryPath := temporaryFile.Name()
	defer os.Remove(temporaryPath)

	if err := temporaryFile.Chmod(0o600); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("set temporary settings file permissions: %w", err)
	}
	if _, err := temporaryFile.Write(data); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("write temporary settings file: %w", err)
	}
	if err := temporaryFile.Sync(); err != nil {
		temporaryFile.Close()
		return fmt.Errorf("sync temporary settings file: %w", err)
	}
	if err := temporaryFile.Close(); err != nil {
		return fmt.Errorf("close temporary settings file: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace settings file: %w", err)
	}

	return nil
}

func randomID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return hex.EncodeToString(bytes), nil
}
