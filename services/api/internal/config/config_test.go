package config

import (
	"testing"
	"time"
)

func TestDefaultCredentialTTLsAreExplicit(t *testing.T) {
	cfg := Default()
	if cfg.RoomSessionTokenTTL != 2*time.Hour {
		t.Fatalf("RoomSessionTokenTTL = %v, want 2h", cfg.RoomSessionTokenTTL)
	}
	if cfg.LiveKitTokenTTL != 10*time.Minute {
		t.Fatalf("LiveKitTokenTTL = %v, want 10m", cfg.LiveKitTokenTTL)
	}
}

func TestFromEnvLoadsDocumentedEnvironmentValues(t *testing.T) {
	t.Setenv("ECHO_HTTP_ADDR", ":18080")
	t.Setenv("ECHO_DATABASE_PATH", "./env-echo.sqlite3")
	t.Setenv("ECHO_LIVEKIT_URL", "wss://livekit.env.test")
	t.Setenv("ECHO_LIVEKIT_API_KEY", "env-livekit-key")
	t.Setenv("ECHO_LIVEKIT_API_SECRET", "env-livekit-secret")
	t.Setenv("ECHO_ROOM_SESSION_SECRET", "env-room-session-secret")
	t.Setenv("ECHO_LOG_DIR", "./env-logs")

	cfg := FromEnv()

	if cfg.HTTPAddr != ":18080" {
		t.Fatalf("HTTPAddr = %q, want env value", cfg.HTTPAddr)
	}
	if cfg.DatabasePath != "./env-echo.sqlite3" {
		t.Fatalf("DatabasePath = %q, want env value", cfg.DatabasePath)
	}
	if cfg.LiveKitURL != "wss://livekit.env.test" {
		t.Fatalf("LiveKitURL = %q, want env value", cfg.LiveKitURL)
	}
	if cfg.LiveKitAPIKey != "env-livekit-key" {
		t.Fatalf("LiveKitAPIKey = %q, want env value", cfg.LiveKitAPIKey)
	}
	if cfg.LiveKitAPISecret != "env-livekit-secret" {
		t.Fatalf("LiveKitAPISecret = %q, want env value", cfg.LiveKitAPISecret)
	}
	if cfg.RoomSessionSecret != "env-room-session-secret" {
		t.Fatalf("RoomSessionSecret = %q, want env value", cfg.RoomSessionSecret)
	}
	if cfg.LogDir != "./env-logs" {
		t.Fatalf("LogDir = %q, want env value", cfg.LogDir)
	}
	if cfg.RoomSessionTokenTTL != 2*time.Hour {
		t.Fatalf("RoomSessionTokenTTL = %v, want 2h", cfg.RoomSessionTokenTTL)
	}
	if cfg.LiveKitTokenTTL != 10*time.Minute {
		t.Fatalf("LiveKitTokenTTL = %v, want 10m", cfg.LiveKitTokenTTL)
	}
}
