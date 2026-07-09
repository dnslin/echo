package config

import (
	"os"
	"strings"
	"time"
)

// Config names the bootstrap settings the API service will need as later issues
// add persistence, LiveKit, session tokens, and file logging.
type Config struct {
	HTTPAddr            string
	DatabasePath        string
	LiveKitURL          string
	LiveKitAPIKey       string
	LiveKitAPISecret    string
	RoomSessionSecret   string
	RoomSessionTokenTTL time.Duration
	LiveKitTokenTTL     time.Duration
	LogDir              string
}

func Default() Config {
	return Config{
		HTTPAddr:            ":8080",
		DatabasePath:        "./echo.sqlite3",
		RoomSessionTokenTTL: 2 * time.Hour,
		LiveKitTokenTTL:     10 * time.Minute,
		LogDir:              "./logs",
	}
}

func FromEnv() Config {
	cfg := Default()
	overlayString(&cfg.HTTPAddr, "ECHO_HTTP_ADDR")
	overlayString(&cfg.DatabasePath, "ECHO_DATABASE_PATH")
	overlayString(&cfg.LiveKitURL, "ECHO_LIVEKIT_URL")
	overlayString(&cfg.LiveKitAPIKey, "ECHO_LIVEKIT_API_KEY")
	overlayString(&cfg.LiveKitAPISecret, "ECHO_LIVEKIT_API_SECRET")
	overlayString(&cfg.RoomSessionSecret, "ECHO_ROOM_SESSION_SECRET")
	overlayString(&cfg.LogDir, "ECHO_LOG_DIR")
	return cfg
}

func overlayString(target *string, key string) {
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" {
		*target = value
	}
}
