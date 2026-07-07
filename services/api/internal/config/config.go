package config

// Config names the bootstrap settings the API service will need as later issues
// add persistence, LiveKit, session tokens, and file logging.
type Config struct {
	HTTPAddr          string
	DatabasePath      string
	LiveKitURL        string
	LiveKitAPIKey     string
	LiveKitAPISecret  string
	RoomSessionSecret string
	LogDir            string
}

func Default() Config {
	return Config{
		HTTPAddr:     ":8080",
		DatabasePath: "./echo.sqlite3",
		LogDir:       "./logs",
	}
}
