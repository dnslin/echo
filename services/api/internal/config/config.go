package config

import (
	"os"
	"strings"
	"time"
)

// Config names the bootstrap settings the API service will need as later issues
// add persistence, LiveKit, session tokens, and file logging.
type Config struct {
	HTTPAddr                string
	DatabasePath            string
	LiveKitURL              string
	LiveKitAPIKey           string
	LiveKitAPISecret        string
	RoomSessionSecret       string
	RoomSessionTokenTTL     time.Duration
	LiveKitTokenTTL         time.Duration
	WebSocketOriginPatterns []string
	HTTPOriginPatterns      []string
	LogDir                  string
}

var defaultWebSocketOriginPatterns = []string{
	"localhost:*",
	"127.0.0.1:*",
	"wails.localhost:*",
	"wails://wails.localhost",
	"wails://wails.localhost:*",
}

var defaultHTTPOriginPatterns = []string{
	"http://localhost:*",
	"http://127.0.0.1:*",
	"http://wails.localhost:*",
	"wails://wails.localhost",
	"wails://wails.localhost:*",
}

func Default() Config {
	return Config{
		HTTPAddr:                ":8080",
		DatabasePath:            "./echo.sqlite3",
		RoomSessionTokenTTL:     2 * time.Hour,
		LiveKitTokenTTL:         10 * time.Minute,
		WebSocketOriginPatterns: append([]string(nil), defaultWebSocketOriginPatterns...),
		HTTPOriginPatterns:      append([]string(nil), defaultHTTPOriginPatterns...),
		LogDir:                  "./logs",
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
	overlayStringSlice(&cfg.WebSocketOriginPatterns, "ECHO_WS_ORIGIN_PATTERNS")
	overlayStringSlice(&cfg.HTTPOriginPatterns, "ECHO_HTTP_ORIGIN_PATTERNS")
	overlayString(&cfg.LogDir, "ECHO_LOG_DIR")
	return cfg
}

func overlayString(target *string, key string) {
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" {
		*target = value
	}
}

func overlayStringSlice(target *[]string, key string) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return
	}
	parts := strings.Split(value, ",")
	parsed := make([]string, 0, len(parts))
	for _, part := range parts {
		pattern := strings.TrimSpace(part)
		if pattern != "" {
			parsed = append(parsed, pattern)
		}
	}
	if len(parsed) > 0 {
		*target = parsed
	}
}
