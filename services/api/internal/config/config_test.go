package config

import (
	"testing"
	"time"
)

func TestDefaultWebSocketOriginPatternsAreExplicitAndNotWildcard(t *testing.T) {
	cfg := Default()
	if len(cfg.WebSocketOriginPatterns) == 0 {
		t.Fatalf("WebSocketOriginPatterns is empty, want explicit local/WebView2 defaults")
	}
	for _, pattern := range cfg.WebSocketOriginPatterns {
		if pattern == "*" {
			t.Fatalf("WebSocketOriginPatterns contains wildcard '*', want bounded origins")
		}
	}
}

func TestDefaultHTTPOriginPatternsAreExplicitAndNotWildcard(t *testing.T) {
	cfg := Default()
	wantOrigins := []string{
		"http://localhost:*",
		"http://127.0.0.1:*",
		"http://wails.localhost:*",
		"wails://wails.localhost",
		"wails://wails.localhost:*",
	}
	if len(cfg.HTTPOriginPatterns) != len(wantOrigins) {
		t.Fatalf("HTTPOriginPatterns = %#v, want %#v", cfg.HTTPOriginPatterns, wantOrigins)
	}
	for index, want := range wantOrigins {
		if cfg.HTTPOriginPatterns[index] != want {
			t.Fatalf("HTTPOriginPatterns[%d] = %q, want %q", index, cfg.HTTPOriginPatterns[index], want)
		}
	}
}

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
	t.Setenv("ECHO_WS_ORIGIN_PATTERNS", "https://client.example, http://localhost:5173 ")
	t.Setenv("ECHO_HTTP_ORIGIN_PATTERNS", "https://desktop-client.example, wails://wails.localhost")
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
	wantOrigins := []string{"https://client.example", "http://localhost:5173"}
	if len(cfg.WebSocketOriginPatterns) != len(wantOrigins) {
		t.Fatalf("WebSocketOriginPatterns = %#v, want %#v", cfg.WebSocketOriginPatterns, wantOrigins)
	}
	for i, want := range wantOrigins {
		if cfg.WebSocketOriginPatterns[i] != want {
			t.Fatalf("WebSocketOriginPatterns[%d] = %q, want %q", i, cfg.WebSocketOriginPatterns[i], want)
		}
	}
	wantHTTPOrigins := []string{"https://desktop-client.example", "wails://wails.localhost"}
	if len(cfg.HTTPOriginPatterns) != len(wantHTTPOrigins) {
		t.Fatalf("HTTPOriginPatterns = %#v, want %#v", cfg.HTTPOriginPatterns, wantHTTPOrigins)
	}
	for i, want := range wantHTTPOrigins {
		if cfg.HTTPOriginPatterns[i] != want {
			t.Fatalf("HTTPOriginPatterns[%d] = %q, want %q", i, cfg.HTTPOriginPatterns[i], want)
		}
	}
	if cfg.RoomSessionTokenTTL != 2*time.Hour {
		t.Fatalf("RoomSessionTokenTTL = %v, want 2h", cfg.RoomSessionTokenTTL)
	}
	if cfg.LiveKitTokenTTL != 10*time.Minute {
		t.Fatalf("LiveKitTokenTTL = %v, want 10m", cfg.LiveKitTokenTTL)
	}
}
