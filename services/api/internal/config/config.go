package config

import "time"

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
