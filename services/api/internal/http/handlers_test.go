package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"echo/services/api/internal/invite"
	"echo/services/api/internal/room"
	"echo/services/api/internal/store"
)

func TestCreateRoomReturnsCreatedRoomAndHostMember(t *testing.T) {
	router := newCreateRoomTestRouter(t)
	payload := map[string]string{
		"anonymous_id": "anon_local_123",
		"nickname":     " Alice ",
		"avatar_id":    "avatar_07",
		"room_name":    " 今晚开黑 ",
	}

	response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms", payload)

	if response.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want %d, body: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	var body createRoomResponseBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("POST /v1/rooms returned invalid JSON: %v", err)
	}
	if len(body.Room.InviteCode) != 6 {
		t.Fatalf("invite code length = %d, want 6", len(body.Room.InviteCode))
	}
	for _, ch := range body.Room.InviteCode {
		if !strings.ContainsRune(invite.CharSet, ch) {
			t.Fatalf("invite code contains invalid character %q in %q", ch, body.Room.InviteCode)
		}
	}
	if body.Room.State != "active" || body.Room.CreatedAt.IsZero() {
		t.Fatalf("room state/time = %q/%v, want active and created_at", body.Room.State, body.Room.CreatedAt)
	}
	if body.Room.LastEmptyAt != nil || body.Room.ExpiresAt != nil {
		t.Fatalf("room last_empty_at/expires_at = %v/%v, want nil/nil", body.Room.LastEmptyAt, body.Room.ExpiresAt)
	}
	if !body.Member.IsHost || body.Member.RoomID != body.Room.ID {
		t.Fatalf("member host/room = %v/%q, want host in created room %q", body.Member.IsHost, body.Member.RoomID, body.Room.ID)
	}
	if body.Member.Nickname != "Alice" || body.Member.State != "online" || body.Member.Muted || body.Member.Speaking || body.Member.VoiceMode != "push_to_talk" {
		t.Fatalf("member initial state = %#v, want normalized online unmuted not speaking push_to_talk", body.Member)
	}
}

func TestCreateRoomValidationErrors(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]string
		wantCode    string
		wantMessage string
	}{
		{
			name: "empty anonymous id",
			payload: map[string]string{
				"anonymous_id": "   ",
				"nickname":     "Alice",
				"avatar_id":    "avatar_07",
			},
			wantCode:    "invalid_anonymous_id",
			wantMessage: "匿名身份不能为空",
		},
		{
			name: "empty avatar id",
			payload: map[string]string{
				"anonymous_id": "anon_local_123",
				"nickname":     "Alice",
				"avatar_id":    "   ",
			},
			wantCode:    "invalid_avatar_id",
			wantMessage: "请选择头像",
		},
		{
			name: "empty nickname",
			payload: map[string]string{
				"anonymous_id": "anon_local_123",
				"nickname":     "   ",
				"avatar_id":    "avatar_07",
			},
			wantCode:    "invalid_nickname",
			wantMessage: "请输入昵称",
		},
		{
			name: "nickname too long",
			payload: map[string]string{
				"anonymous_id": "anon_local_123",
				"nickname":     strings.Repeat("你", 17),
				"avatar_id":    "avatar_07",
			},
			wantCode:    "nickname_too_long",
			wantMessage: "昵称最多 16 个字符",
		},
		{
			name: "room name too long",
			payload: map[string]string{
				"anonymous_id": "anon_local_123",
				"nickname":     "Alice",
				"avatar_id":    "avatar_07",
				"room_name":    strings.Repeat("房", 25),
			},
			wantCode:    "room_name_too_long",
			wantMessage: "房间名称最多 24 个字符",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newCreateRoomTestRouter(t)
			response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms", tt.payload)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("POST /v1/rooms status = %d, want %d, body: %s", response.Code, http.StatusBadRequest, response.Body.String())
			}
			var body errorResponseBody
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatalf("POST /v1/rooms returned invalid error JSON: %v", err)
			}
			if body.Error.Code != tt.wantCode || body.Error.Message != tt.wantMessage {
				t.Fatalf("error response = %s/%s, want %s/%s", body.Error.Code, body.Error.Message, tt.wantCode, tt.wantMessage)
			}
		})
	}
}

func newCreateRoomTestRouter(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "echo.sqlite3"))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db.DB returned error: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	roomService := room.NewService(store.NewRepository(db), invite.NewGenerator())
	return NewRouter(WithRoomCreator(roomService))
}

func performJSONRequest(t *testing.T, handler http.Handler, method string, target string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal payload: %v", err)
	}
	request := httptest.NewRequest(method, target, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type createRoomResponseBody struct {
	Room struct {
		ID          string     `json:"id"`
		Name        string     `json:"name"`
		InviteCode  string     `json:"invite_code"`
		State       string     `json:"state"`
		CreatedAt   time.Time  `json:"created_at"`
		LastEmptyAt *time.Time `json:"last_empty_at"`
		ExpiresAt   *time.Time `json:"expires_at"`
	} `json:"room"`
	Member struct {
		ID              string    `json:"id"`
		RoomID          string    `json:"room_id"`
		AnonymousID     string    `json:"anonymous_id"`
		Nickname        string    `json:"nickname"`
		AvatarID        string    `json:"avatar_id"`
		IsHost          bool      `json:"is_host"`
		State           string    `json:"state"`
		Muted           bool      `json:"muted"`
		Speaking        bool      `json:"speaking"`
		VoiceMode       string    `json:"voice_mode"`
		LiveKitIdentity string    `json:"livekit_identity"`
		JoinedAt        time.Time `json:"joined_at"`
	} `json:"member"`
}

type errorResponseBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
