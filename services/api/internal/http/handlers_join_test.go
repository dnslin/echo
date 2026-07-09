package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"echo/services/api/internal/domain"
	"echo/services/api/internal/invite"
	"echo/services/api/internal/room"
	"echo/services/api/internal/store"
)

func TestJoinRoomReturnsExistingRoomAndNonHostMember(t *testing.T) {
	router, _ := newJoinRoomIntegration(t)
	createPayload := map[string]string{
		"anonymous_id": "anon_local_123",
		"nickname":     "Alice",
		"avatar_id":    "avatar_07",
		"room_name":    "今晚开黑",
	}
	createResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms", createPayload)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want %d, body_bytes=%d", createResponse.Code, http.StatusCreated, createResponse.Body.Len())
	}
	created := decodeCreateRoomResponse(t, createResponse)

	joinPayload := map[string]string{
		"invite_code":  noisyInvite(created.Room.InviteCode),
		"anonymous_id": " anon_local_456 ",
		"nickname":     " Alice ",
		"avatar_id":    " avatar_08 ",
	}
	joinResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", joinPayload)
	if joinResponse.Code != http.StatusOK {
		t.Fatalf("POST /v1/rooms/join status = %d, want %d, body_bytes=%d", joinResponse.Code, http.StatusOK, joinResponse.Body.Len())
	}
	joined := decodeCreateRoomResponse(t, joinResponse)

	if joined.Room.ID != created.Room.ID || joined.Room.InviteCode != created.Room.InviteCode || joined.Room.State != "active" {
		t.Fatalf("joined room = %#v, want created room id/code active", joined.Room)
	}
	if joined.Member.IsHost || joined.Member.RoomID != created.Room.ID {
		t.Fatalf("joined member host/room = %v/%q, want non-host in room %q", joined.Member.IsHost, joined.Member.RoomID, created.Room.ID)
	}
	if joined.Member.Nickname != "Alice" || joined.Member.AnonymousID != "anon_local_456" || joined.Member.AvatarID != "avatar_08" {
		t.Fatalf("joined member display fields = %#v, want trimmed duplicate nickname join fields", joined.Member)
	}
	if joined.Member.State != "online" || joined.Member.Muted || joined.Member.Speaking || joined.Member.VoiceMode != "push_to_talk" {
		t.Fatalf("joined member state = %#v, want online unmuted not speaking push_to_talk", joined.Member)
	}
}

func TestJoinRoomValidationAndProductErrors(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "empty invite code",
			payload:     map[string]string{"invite_code": " - \t ", "anonymous_id": "anon_local_456", "nickname": "Alice", "avatar_id": "avatar_08"},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "empty_invite_code",
			wantMessage: "请输入邀请码",
		},
		{
			name:        "invalid invite format",
			payload:     map[string]string{"invite_code": "ABC12!", "anonymous_id": "anon_local_456", "nickname": "Alice", "avatar_id": "avatar_08"},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_invite_format",
			wantMessage: "邀请码应为 6 位字母或数字",
		},
		{
			name:        "invalid invite length",
			payload:     map[string]string{"invite_code": "ABC12", "anonymous_id": "anon_local_456", "nickname": "Alice", "avatar_id": "avatar_08"},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_invite_format",
			wantMessage: "邀请码应为 6 位字母或数字",
		},
		{
			name:        "unknown invite",
			payload:     map[string]string{"invite_code": "ZZZZZZ", "anonymous_id": "anon_local_456", "nickname": "Alice", "avatar_id": "avatar_08"},
			wantStatus:  http.StatusNotFound,
			wantCode:    "invite_not_found",
			wantMessage: "邀请码无效，请检查后重试",
		},
		{
			name:        "empty anonymous id reuses create-room validation",
			payload:     map[string]string{"invite_code": "ABC123", "anonymous_id": " ", "nickname": "Alice", "avatar_id": "avatar_08"},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_anonymous_id",
			wantMessage: "匿名身份不能为空",
		},
		{
			name:        "anonymous id too long reuses create-room validation",
			payload:     map[string]string{"invite_code": "ABC123", "anonymous_id": strings.Repeat("a", 129), "nickname": "Alice", "avatar_id": "avatar_08"},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "anonymous_id_too_long",
			wantMessage: "匿名身份最多 128 个字符",
		},
		{
			name:        "empty avatar id reuses create-room validation",
			payload:     map[string]string{"invite_code": "ABC123", "anonymous_id": "anon_local_456", "nickname": "Alice", "avatar_id": " "},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_avatar_id",
			wantMessage: "请选择头像",
		},
		{
			name:        "avatar id too long reuses create-room validation",
			payload:     map[string]string{"invite_code": "ABC123", "anonymous_id": "anon_local_456", "nickname": "Alice", "avatar_id": strings.Repeat("a", 65)},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "avatar_id_too_long",
			wantMessage: "头像标识最多 64 个字符",
		},
		{
			name:        "empty nickname reuses create-room validation",
			payload:     map[string]string{"invite_code": "ABC123", "anonymous_id": "anon_local_456", "nickname": " ", "avatar_id": "avatar_08"},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_nickname",
			wantMessage: "请输入昵称",
		},
		{
			name:        "nickname too long reuses create-room validation",
			payload:     map[string]string{"invite_code": "ABC123", "anonymous_id": "anon_local_456", "nickname": strings.Repeat("你", 17), "avatar_id": "avatar_08"},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "nickname_too_long",
			wantMessage: "昵称最多 16 个字符",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := newJoinRoomIntegration(t)
			response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", tt.payload)
			assertHTTPError(t, response, tt.wantStatus, tt.wantCode, tt.wantMessage)
		})
	}
}

func TestJoinRoomRejectsExpiredRoom(t *testing.T) {
	router, repository := newJoinRoomIntegration(t)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	roomToJoin := apiTestRoom("room_expired_http", "EXP123", domain.RoomStateExpired, nil, now)
	if err := repository.CreateRoomWithMember(context.Background(), roomToJoin, apiTestMember("mem_expired_http", roomToJoin.ID, domain.MemberStateOnline, true, now)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}

	response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  "EXP123",
		"anonymous_id": "anon_local_456",
		"nickname":     "Alice",
		"avatar_id":    "avatar_08",
	})

	assertHTTPError(t, response, http.StatusGone, "room_expired", "该房间已过期，请让朋友重新创建")
}

func TestJoinRoomRejectsFullRoom(t *testing.T) {
	router, repository := newJoinRoomIntegration(t)
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	roomToJoin := apiTestRoom("room_full_http", "FULL10", domain.RoomStateActive, nil, now)
	if err := repository.CreateRoomWithMember(context.Background(), roomToJoin, apiTestMember("mem_full_host", roomToJoin.ID, domain.MemberStateOnline, true, now)); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	for i := 1; i < 10; i++ {
		member := apiTestMember("mem_full_"+string(rune('0'+i)), roomToJoin.ID, domain.MemberStateOnline, false, now)
		member.AnonymousID = "anon_full_" + string(rune('0'+i))
		if err := repository.CreateMember(context.Background(), member); err != nil {
			t.Fatalf("CreateMember(%s) returned error: %v", member.ID, err)
		}
	}

	response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  "FULL10",
		"anonymous_id": "anon_local_999",
		"nickname":     "Alice",
		"avatar_id":    "avatar_08",
	})

	assertHTTPError(t, response, http.StatusConflict, "room_full", "房间人数已满，暂时无法加入")
}

func TestJoinRoomPassesRequestContext(t *testing.T) {
	joiner := &captureContextRoomJoiner{}
	router := NewRouter(WithRoomJoiner(joiner), WithCredentialConfig(testCredentialConfig()))
	request := httptest.NewRequest(http.MethodPost, "/v1/rooms/join", strings.NewReader(`{"invite_code":"ABC123","anonymous_id":"anon_local_456","nickname":"Alice","avatar_id":"avatar_08"}`))
	request.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(request.Context(), testContextKey{}, "join-request-context")
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("POST /v1/rooms/join status = %d, want %d, body_bytes=%d", response.Code, http.StatusOK, response.Body.Len())
	}
	if joiner.contextValue != "join-request-context" {
		t.Fatalf("joiner context value = %v, want join-request-context", joiner.contextValue)
	}
}

func TestJoinRoomRejectsMalformedAndOversizedRequestBeforeService(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{`},
		{name: "oversized", body: `{"invite_code":"ABC123","anonymous_id":"anon_local_456","nickname":"` + strings.Repeat("a", maxCreateRoomRequestBytes) + `","avatar_id":"avatar_08"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			joiner := &captureContextRoomJoiner{}
			router := NewRouter(WithRoomJoiner(joiner), WithCredentialConfig(testCredentialConfig()))
			request := httptest.NewRequest(http.MethodPost, "/v1/rooms/join", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if joiner.calls != 0 {
				t.Fatalf("room joiner calls = %d, want 0", joiner.calls)
			}
			assertHTTPError(t, response, http.StatusBadRequest, "invalid_request", "请求格式无效")
		})
	}
}

func TestJoinRoomMapsUnexpectedServiceError(t *testing.T) {
	joiner := &captureContextRoomJoiner{err: errors.New("store failed")}
	router := NewRouter(WithRoomJoiner(joiner), WithCredentialConfig(testCredentialConfig()))
	response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  "ABC123",
		"anonymous_id": "anon_local_456",
		"nickname":     "Alice",
		"avatar_id":    "avatar_08",
	})

	assertHTTPError(t, response, http.StatusInternalServerError, "internal_error", "服务器错误")
}

func newJoinRoomIntegration(t *testing.T) (http.Handler, *store.Repository) {
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
	repository := store.NewRepository(db)
	roomService := room.NewService(repository, invite.NewGenerator())
	return NewRouter(WithRoomCreator(roomService), WithRoomJoiner(roomService), WithCredentialConfig(testCredentialConfig())), repository
}

func decodeCreateRoomResponse(t *testing.T, response *httptest.ResponseRecorder) createRoomResponseBody {
	t.Helper()
	var body createRoomResponseBody
	if err := jsonUnmarshalResponse(response, &body); err != nil {
		t.Fatalf("response returned invalid JSON: %v", err)
	}
	return body
}

func jsonUnmarshalResponse(response *httptest.ResponseRecorder, out any) error {
	return json.Unmarshal(response.Body.Bytes(), out)
}

func assertHTTPError(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, wantCode string, wantMessage string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("status = %d, want %d, body_bytes=%d", response.Code, wantStatus, response.Body.Len())
	}
	var body errorResponseBody
	if err := jsonUnmarshalResponse(response, &body); err != nil {
		t.Fatalf("error response returned invalid JSON: %v", err)
	}
	if body.Error.Code != wantCode || body.Error.Message != wantMessage {
		t.Fatalf("error response = %s/%s, want %s/%s", body.Error.Code, body.Error.Message, wantCode, wantMessage)
	}
}

func noisyInvite(code string) string {
	return strings.ToLower(code[:2] + "-" + code[2:4] + " " + code[4:])
}

func apiTestRoom(id string, inviteCode string, state domain.RoomState, expiresAt *time.Time, now time.Time) domain.Room {
	return domain.Room{
		ID:              id,
		Name:            "今晚开黑",
		InviteCode:      inviteCode,
		LiveKitRoomName: "lk_" + id,
		HostAnonymousID: "anon_local_123",
		HostNickname:    "Alice",
		HostAvatarID:    "avatar_07",
		State:           state,
		CreatedAt:       now,
		ExpiresAt:       expiresAt,
		UpdatedAt:       now,
	}
}

func apiTestMember(id string, roomID string, state domain.MemberState, isHost bool, now time.Time) domain.Member {
	return domain.Member{
		ID:              id,
		RoomID:          roomID,
		AnonymousID:     id + "_anon",
		Nickname:        "Alice",
		AvatarID:        "avatar_08",
		IsHost:          isHost,
		State:           state,
		Muted:           false,
		Speaking:        false,
		VoiceMode:       domain.VoiceModePushToTalk,
		JoinedAt:        now,
		LiveKitIdentity: id,
	}
}

type captureContextRoomJoiner struct {
	calls        int
	contextValue any
	err          error
}

func (c *captureContextRoomJoiner) JoinContext(ctx context.Context, input room.JoinInput) (room.JoinResult, error) {
	c.calls++
	c.contextValue = ctx.Value(testContextKey{})
	if c.err != nil {
		return room.JoinResult{}, c.err
	}
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	return room.JoinResult{
		Room: domain.Room{
			ID:              "room_test",
			Name:            "临时房间",
			InviteCode:      "ABC123",
			LiveKitRoomName: "lk_room_test",
			State:           domain.RoomStateActive,
			CreatedAt:       now,
		},
		Member: domain.Member{
			ID:              "mem_join_test",
			RoomID:          "room_test",
			AnonymousID:     input.AnonymousID,
			Nickname:        input.Nickname,
			AvatarID:        input.AvatarID,
			IsHost:          false,
			State:           domain.MemberStateOnline,
			VoiceMode:       domain.VoiceModePushToTalk,
			LiveKitIdentity: "mem_join_test",
			JoinedAt:        now,
		},
	}, nil
}
