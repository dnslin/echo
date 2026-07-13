package app

import "echo/apps/desktop/internal/config"

type SettingsStore interface {
	Load() (config.Settings, error)
	Save(config.Settings) error
	ResetAvatar() (config.Settings, error)
}

type SettingsService struct {
	store SettingsStore
}

func NewSettingsService(store SettingsStore) *SettingsService {
	return &SettingsService{store: store}
}

func (s *SettingsService) Load() (config.Settings, error) {
	return s.store.Load()
}

func (s *SettingsService) Save(settings config.Settings) (config.Settings, error) {
	if err := s.store.Save(settings); err != nil {
		return config.Settings{}, err
	}

	return s.store.Load()
}

func (s *SettingsService) ResetAvatar() (config.Settings, error) {
	return s.store.ResetAvatar()
}
