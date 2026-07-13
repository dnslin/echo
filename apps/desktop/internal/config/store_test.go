package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesUsablePersistedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	settings, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if settings.AnonymousID == "" {
		t.Fatal("Load() AnonymousID is empty")
	}
	if settings.AvatarID == "" {
		t.Fatal("Load() AvatarID is empty")
	}
	if settings.PushToTalkKey != DefaultPushToTalkKey {
		t.Fatalf("Load() PushToTalkKey = %q, want %q", settings.PushToTalkKey, DefaultPushToTalkKey)
	}
	if settings.VoiceMode != VoiceModePushToTalk {
		t.Fatalf("Load() VoiceMode = %q, want %q", settings.VoiceMode, VoiceModePushToTalk)
	}
	if settings.OutputVolume != DefaultOutputVolume {
		t.Fatalf("Load() OutputVolume = %d, want %d", settings.OutputVolume, DefaultOutputVolume)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var persisted map[string]json.RawMessage
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	for _, field := range []string{
		"anonymous_id",
		"nickname",
		"avatar_id",
		"push_to_talk_key",
		"microphone_device",
		"output_device",
		"voice_mode",
		"output_volume",
	} {
		if _, ok := persisted[field]; !ok {
			t.Errorf("persisted JSON is missing %q", field)
		}
	}
}

func TestLoadRestoresAllSavedSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	want := Settings{
		AnonymousID:      "local-anonymous-id",
		Nickname:         "echo",
		AvatarID:         "random-avatar-id",
		PushToTalkKey:    "B",
		MicrophoneDevice: "microphone-id",
		OutputDevice:     "output-id",
		VoiceMode:        VoiceModeFreeTalk,
		OutputVolume:     37,
	}

	if err := NewStore(path).Save(want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	got, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
}

func TestResetAvatarChangesOnlyAvatarAndPersists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	original := Settings{
		AnonymousID:      "local-anonymous-id",
		Nickname:         "echo",
		AvatarID:         "original-avatar-id",
		PushToTalkKey:    "B",
		MicrophoneDevice: "microphone-id",
		OutputDevice:     "output-id",
		VoiceMode:        VoiceModeFreeTalk,
		OutputVolume:     37,
	}
	store := NewStore(path)
	if err := store.Save(original); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	reset, err := store.ResetAvatar()
	if err != nil {
		t.Fatalf("ResetAvatar() error = %v", err)
	}
	if reset.AvatarID == "" || reset.AvatarID == original.AvatarID {
		t.Fatalf("ResetAvatar() AvatarID = %q, want a new non-empty value", reset.AvatarID)
	}
	if reset.AnonymousID != original.AnonymousID ||
		reset.Nickname != original.Nickname ||
		reset.PushToTalkKey != original.PushToTalkKey ||
		reset.MicrophoneDevice != original.MicrophoneDevice ||
		reset.OutputDevice != original.OutputDevice ||
		reset.VoiceMode != original.VoiceMode ||
		reset.OutputVolume != original.OutputVolume {
		t.Fatalf("ResetAvatar() changed settings other than AvatarID: %#v", reset)
	}

	reloaded, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if reloaded != reset {
		t.Fatalf("Load() = %#v, want %#v", reloaded, reset)
	}
}

func TestLoadRecoversFromMalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"anonymous_id":`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	recovered, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	assertUsableDefaults(t, recovered)

	reloaded, err := NewStore(path).Load()
	if err != nil {
		t.Fatalf("Load() after recovery error = %v", err)
	}
	if reloaded != recovered {
		t.Fatalf("Load() after recovery = %#v, want %#v", reloaded, recovered)
	}
}

func TestLoadNormalizesMissingAndInvalidSafetyFields(t *testing.T) {
	tests := []struct {
		name       string
		contents   string
		wantVolume int
	}{
		{
			name:       "missing fields receive defaults",
			contents:   `{}`,
			wantVolume: DefaultOutputVolume,
		},
		{
			name:       "invalid fields are repaired",
			contents:   `{"anonymous_id":"","avatar_id":"","push_to_talk_key":"  ","voice_mode":"always_on","output_volume":101}`,
			wantVolume: MaximumOutputVolume,
		},
		{
			name:       "negative volume is clamped",
			contents:   `{"output_volume":-1}`,
			wantVolume: MinimumOutputVolume,
		},
		{
			name:       "explicit muted volume remains valid",
			contents:   `{"output_volume":0}`,
			wantVolume: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, []byte(test.contents), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}

			settings, err := NewStore(path).Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if settings.AnonymousID == "" || settings.AvatarID == "" {
				t.Fatalf("Load() returned unusable identity: %#v", settings)
			}
			if settings.PushToTalkKey != DefaultPushToTalkKey {
				t.Fatalf("Load() PushToTalkKey = %q, want %q", settings.PushToTalkKey, DefaultPushToTalkKey)
			}
			if settings.VoiceMode != VoiceModePushToTalk {
				t.Fatalf("Load() VoiceMode = %q, want %q", settings.VoiceMode, VoiceModePushToTalk)
			}
			if settings.OutputVolume != test.wantVolume {
				t.Fatalf("Load() OutputVolume = %d, want %d", settings.OutputVolume, test.wantVolume)
			}

			reloaded, err := NewStore(path).Load()
			if err != nil {
				t.Fatalf("Load() after normalization error = %v", err)
			}
			if reloaded != settings {
				t.Fatalf("Load() after normalization = %#v, want %#v", reloaded, settings)
			}
		})
	}
}

func TestLoadReturnsReadErrors(t *testing.T) {
	_, err := NewStore(t.TempDir()).Load()
	if err == nil {
		t.Fatal("Load() error = nil, want an I/O error")
	}
}

func TestSaveReturnsWriteErrors(t *testing.T) {
	directory := t.TempDir()
	blocker := filepath.Join(directory, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("blocker"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	err := NewStore(filepath.Join(blocker, "settings.json")).Save(Settings{})
	if err == nil {
		t.Fatal("Save() error = nil, want an I/O error")
	}
}

func assertUsableDefaults(t *testing.T, settings Settings) {
	t.Helper()

	if settings.AnonymousID == "" || settings.AvatarID == "" {
		t.Fatalf("settings identity is unusable: %#v", settings)
	}
	if settings.PushToTalkKey != DefaultPushToTalkKey {
		t.Fatalf("PushToTalkKey = %q, want %q", settings.PushToTalkKey, DefaultPushToTalkKey)
	}
	if settings.VoiceMode != VoiceModePushToTalk {
		t.Fatalf("VoiceMode = %q, want %q", settings.VoiceMode, VoiceModePushToTalk)
	}
	if settings.OutputVolume != DefaultOutputVolume {
		t.Fatalf("OutputVolume = %d, want %d", settings.OutputVolume, DefaultOutputVolume)
	}
}
