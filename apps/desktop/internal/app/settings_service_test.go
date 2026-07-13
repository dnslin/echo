package app

import (
	"errors"
	"testing"

	"echo/apps/desktop/internal/config"
)

func TestSettingsServiceLoadsSettings(t *testing.T) {
	want := config.Settings{AnonymousID: "anon", Nickname: "小王"}
	store := &fakeSettingsStore{loadResult: want}

	got, err := NewSettingsService(store).Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got != want {
		t.Fatalf("Load() = %#v, want %#v", got, want)
	}
	if store.loadCalls != 1 {
		t.Fatalf("Load() calls = %d, want 1", store.loadCalls)
	}
}

func TestSettingsServiceSavesThenReturnsPersistedSettings(t *testing.T) {
	input := config.Settings{AnonymousID: "anon", Nickname: "输入昵称", OutputVolume: 101}
	persisted := config.Settings{AnonymousID: "anon", Nickname: "输入昵称", OutputVolume: config.MaximumOutputVolume}
	store := &fakeSettingsStore{loadResult: persisted}

	got, err := NewSettingsService(store).Save(input)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if store.saved != input {
		t.Fatalf("Save() store input = %#v, want %#v", store.saved, input)
	}
	if store.saveCalls != 1 || store.loadCalls != 1 {
		t.Fatalf("Save() calls = save:%d load:%d, want save:1 load:1", store.saveCalls, store.loadCalls)
	}
	if got != persisted {
		t.Fatalf("Save() = %#v, want persisted %#v", got, persisted)
	}
}

func TestSettingsServiceResetsAvatar(t *testing.T) {
	want := config.Settings{AnonymousID: "anon", AvatarID: "new-avatar"}
	store := &fakeSettingsStore{resetResult: want}

	got, err := NewSettingsService(store).ResetAvatar()
	if err != nil {
		t.Fatalf("ResetAvatar() error = %v", err)
	}
	if got != want {
		t.Fatalf("ResetAvatar() = %#v, want %#v", got, want)
	}
	if store.resetCalls != 1 {
		t.Fatalf("ResetAvatar() calls = %d, want 1", store.resetCalls)
	}
}

func TestSettingsServicePropagatesStoreErrors(t *testing.T) {
	loadErr := errors.New("load failed")
	saveErr := errors.New("save failed")
	resetErr := errors.New("reset failed")

	if _, err := NewSettingsService(&fakeSettingsStore{loadErr: loadErr}).Load(); !errors.Is(err, loadErr) {
		t.Fatalf("Load() error = %v, want %v", err, loadErr)
	}
	if _, err := NewSettingsService(&fakeSettingsStore{saveErr: saveErr}).Save(config.Settings{}); !errors.Is(err, saveErr) {
		t.Fatalf("Save() error = %v, want %v", err, saveErr)
	}
	if _, err := NewSettingsService(&fakeSettingsStore{loadErr: loadErr}).Save(config.Settings{}); !errors.Is(err, loadErr) {
		t.Fatalf("Save() post-write load error = %v, want %v", err, loadErr)
	}
	if _, err := NewSettingsService(&fakeSettingsStore{resetErr: resetErr}).ResetAvatar(); !errors.Is(err, resetErr) {
		t.Fatalf("ResetAvatar() error = %v, want %v", err, resetErr)
	}
}

type fakeSettingsStore struct {
	loadResult  config.Settings
	resetResult config.Settings
	loadErr     error
	saveErr     error
	resetErr    error
	saved       config.Settings
	loadCalls   int
	saveCalls   int
	resetCalls  int
}

func (s *fakeSettingsStore) Load() (config.Settings, error) {
	s.loadCalls++
	return s.loadResult, s.loadErr
}

func (s *fakeSettingsStore) Save(settings config.Settings) error {
	s.saveCalls++
	s.saved = settings
	return s.saveErr
}

func (s *fakeSettingsStore) ResetAvatar() (config.Settings, error) {
	s.resetCalls++
	return s.resetResult, s.resetErr
}
