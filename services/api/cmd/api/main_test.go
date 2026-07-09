package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"echo/services/api/internal/config"
	"echo/services/api/internal/store"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func TestEnvLoadedStartupRouterCreatesRoomWithCredentials(t *testing.T) {
	t.Setenv("ECHO_HTTP_ADDR", ":18080")
	t.Setenv("ECHO_DATABASE_PATH", filepath.Join(t.TempDir(), "echo.sqlite3"))
	t.Setenv("ECHO_LIVEKIT_URL", "wss://livekit.env.test")
	t.Setenv("ECHO_LIVEKIT_API_KEY", "env-livekit-key")
	t.Setenv("ECHO_LIVEKIT_API_SECRET", "env-livekit-secret")
	t.Setenv("ECHO_ROOM_SESSION_SECRET", "env-room-session-secret")
	t.Setenv("ECHO_WS_ORIGIN_PATTERNS", "https://desktop-client.example")
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
	router := newRouter(cfg, db)

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want %d, body_bytes=%d", response.Code, http.StatusCreated, response.Body.Len())
	}
	var created struct {
		RoomSessionToken string `json:"room_session_token"`
		LiveKitURL       string `json:"livekit_url"`
		LiveKitToken     string `json:"livekit_token"`
		Room             struct {
			ID string `json:"id"`
		} `json:"room"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatalf("response returned invalid JSON: %v", err)
	}
	if created.LiveKitURL != "wss://livekit.env.test" || created.RoomSessionToken == "" || created.LiveKitToken == "" {
		t.Fatalf("credential fields present = url:%t session:%t livekit:%t, want env URL and non-empty tokens", created.LiveKitURL == "wss://livekit.env.test", created.RoomSessionToken != "", created.LiveKitToken != "")
	}

	server := httptest.NewServer(router)
	defer server.Close()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/v1/rooms/" + created.Room.ID + "/ws?token=" + created.RoomSessionToken
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, responseHTTP, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": []string{"https://desktop-client.example"}}})
	if err != nil {
		status := 0
		if responseHTTP != nil {
			status = responseHTTP.StatusCode
		}
		t.Fatalf("websocket dial returned error: %v, status=%d", err, status)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")
	var event struct {
		Type string `json:"type"`
	}
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("websocket read returned error: %v", err)
	}
	if event.Type != "room.snapshot" {
		t.Fatalf("first websocket event type = %q, want room.snapshot", event.Type)
	}
}
