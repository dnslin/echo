package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"echo/services/api/internal/config"
	"echo/services/api/internal/store"
)

func TestEnvLoadedStartupRouterCreatesRoomWithCredentials(t *testing.T) {
	t.Setenv("ECHO_HTTP_ADDR", ":18080")
	t.Setenv("ECHO_DATABASE_PATH", filepath.Join(t.TempDir(), "echo.sqlite3"))
	t.Setenv("ECHO_LIVEKIT_URL", "wss://livekit.env.test")
	t.Setenv("ECHO_LIVEKIT_API_KEY", "env-livekit-key")
	t.Setenv("ECHO_LIVEKIT_API_SECRET", "env-livekit-secret")
	t.Setenv("ECHO_ROOM_SESSION_SECRET", "env-room-session-secret")
	t.Setenv("ECHO_LOG_DIR", filepath.Join(t.TempDir(), "logs"))

	cfg := config.FromEnv()
	db, err := store.OpenSQLite(cfg.DatabasePath)
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	payload := map[string]string{
		"anonymous_id": "anon_local_123",
		"nickname":     "Alice",
		"avatar_id":    "avatar_07",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal payload: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/rooms", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	newRouter(cfg, db).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want %d, body_bytes=%d", response.Code, http.StatusCreated, response.Body.Len())
	}
	var created struct {
		RoomSessionToken string `json:"room_session_token"`
		LiveKitURL       string `json:"livekit_url"`
		LiveKitToken     string `json:"livekit_token"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("response returned invalid JSON: %v", err)
	}
	if created.LiveKitURL != "wss://livekit.env.test" || created.RoomSessionToken == "" || created.LiveKitToken == "" {
		t.Fatalf("credential fields present = url:%t session:%t livekit:%t, want env URL and non-empty tokens", created.LiveKitURL == "wss://livekit.env.test", created.RoomSessionToken != "", created.LiveKitToken != "")
	}
}
