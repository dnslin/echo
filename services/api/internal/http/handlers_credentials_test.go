package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"echo/services/api/internal/domain"
	"echo/services/api/internal/invite"
	"echo/services/api/internal/room"
	"echo/services/api/internal/session"
	"echo/services/api/internal/store"
)

var credentialNow = time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

func TestCreateRoomResponseIncludesCredentials(t *testing.T) {
	router := newCreateRoomTestRouter(t)
	response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms", map[string]string{
		"anonymous_id": "anon_local_123",
		"nickname":     "Alice",
		"avatar_id":    "avatar_07",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want %d, body_bytes=%d", response.Code, http.StatusCreated, response.Body.Len())
	}
	body := decodeCreateRoomResponse(t, response)

	assertCredentialFields(t, body, body.Room.ID, body.Member.ID)
	assertLiveKitTokenScope(t, body.LiveKitToken, "lk_"+body.Room.ID, body.Member.LiveKitIdentity, body.Member.Nickname)
}

func TestJoinRoomResponseIncludesCredentials(t *testing.T) {
	router, _ := newJoinRoomIntegration(t)
	createResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms", map[string]string{
		"anonymous_id": "anon_local_123",
		"nickname":     "Alice",
		"avatar_id":    "avatar_07",
	})
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want %d, body_bytes=%d", createResponse.Code, http.StatusCreated, createResponse.Body.Len())
	}
	created := decodeCreateRoomResponse(t, createResponse)

	joinResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  noisyInvite(created.Room.InviteCode),
		"anonymous_id": "anon_local_456",
		"nickname":     "Bob",
		"avatar_id":    "avatar_08",
	})
	if joinResponse.Code != http.StatusOK {
		t.Fatalf("POST /v1/rooms/join status = %d, want %d, body_bytes=%d", joinResponse.Code, http.StatusOK, joinResponse.Body.Len())
	}
	joined := decodeCreateRoomResponse(t, joinResponse)

	assertCredentialFields(t, joined, joined.Room.ID, joined.Member.ID)
	assertLiveKitTokenScope(t, joined.LiveKitToken, "lk_"+joined.Room.ID, joined.Member.LiveKitIdentity, joined.Member.Nickname)
}

func TestFreshLiveKitTokenSucceedsForActiveMember(t *testing.T) {
	router, _ := newCredentialIntegration(t)
	created := createCredentialRoom(t, router)

	response := performAuthorizedRequest(t, router, http.MethodPost, "/v1/rooms/"+created.Room.ID+"/livekit-token", created.RoomSessionToken)
	if response.Code != http.StatusOK {
		t.Fatalf("POST livekit-token status = %d, want %d, body_bytes=%d", response.Code, http.StatusOK, response.Body.Len())
	}
	body := decodeLiveKitTokenResponse(t, response)
	if body.LiveKitURL != testCredentialConfig().LiveKitURL || body.LiveKitToken == "" {
		t.Fatalf("fresh token fields present = url:%t livekit:%t, want configured URL and non-empty token", body.LiveKitURL == testCredentialConfig().LiveKitURL, body.LiveKitToken != "")
	}
	assertLiveKitTokenScope(t, body.LiveKitToken, "lk_"+created.Room.ID, created.Member.LiveKitIdentity, created.Member.Nickname)
}

func TestFreshLiveKitTokenRejectsInvalidSessionTokens(t *testing.T) {
	router, _ := newCredentialIntegration(t)
	created := createCredentialRoom(t, router)
	expiredToken, _, err := session.Sign(session.SignInput{
		Secret:   testCredentialConfig().RoomSessionSecret,
		RoomID:   created.Room.ID,
		MemberID: created.Member.ID,
		Now:      credentialNow.Add(-3 * time.Hour),
		TTL:      2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Sign expired token: %v", err)
	}

	tests := []struct {
		name        string
		token       string
		pathRoomID  string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{name: "missing bearer", token: "", pathRoomID: created.Room.ID, wantStatus: http.StatusUnauthorized, wantCode: "invalid_room_session", wantMessage: "连接凭证无效，请重新进入房间"},
		{name: "tampered token", token: "A" + created.RoomSessionToken[1:], pathRoomID: created.Room.ID, wantStatus: http.StatusUnauthorized, wantCode: "invalid_room_session", wantMessage: "连接凭证无效，请重新进入房间"},
		{name: "expired token", token: expiredToken, pathRoomID: created.Room.ID, wantStatus: http.StatusUnauthorized, wantCode: "room_session_expired", wantMessage: "连接凭证已过期，请重新进入房间"},
		{name: "room mismatch", token: created.RoomSessionToken, pathRoomID: "room_other", wantStatus: http.StatusForbidden, wantCode: "room_session_mismatch", wantMessage: "连接凭证与房间不匹配"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performAuthorizedRequest(t, router, http.MethodPost, "/v1/rooms/"+tt.pathRoomID+"/livekit-token", tt.token)
			assertHTTPError(t, response, tt.wantStatus, tt.wantCode, tt.wantMessage)
		})
	}
}

func TestFreshLiveKitTokenRejectsInactiveOrMissingProductMember(t *testing.T) {
	router, repository := newCredentialIntegration(t)
	created := createCredentialRoom(t, router)
	missingRoomToken, _, err := session.Sign(session.SignInput{Secret: testCredentialConfig().RoomSessionSecret, RoomID: "room_missing", MemberID: "mem_missing", Now: credentialNow, TTL: 2 * time.Hour})
	if err != nil {
		t.Fatalf("Sign missing room token: %v", err)
	}
	missingMemberToken, _, err := session.Sign(session.SignInput{Secret: testCredentialConfig().RoomSessionSecret, RoomID: created.Room.ID, MemberID: "mem_missing", Now: credentialNow, TTL: 2 * time.Hour})
	if err != nil {
		t.Fatalf("Sign missing member token: %v", err)
	}
	_, _, err = repository.LeaveRoomMember(context.Background(), created.Room.ID, created.Member.ID, []domain.MemberState{domain.MemberStateOnline, domain.MemberStateReconnecting}, credentialNow.Add(time.Minute), 30*time.Minute)
	if err != nil {
		t.Fatalf("LeaveRoomMember returned error: %v", err)
	}

	tests := []struct {
		name        string
		roomID      string
		token       string
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{name: "missing room", roomID: "room_missing", token: missingRoomToken, wantStatus: http.StatusNotFound, wantCode: "room_not_found", wantMessage: "房间不存在或已失效"},
		{name: "missing member", roomID: created.Room.ID, token: missingMemberToken, wantStatus: http.StatusForbidden, wantCode: "member_not_active", wantMessage: "成员不在房间中"},
		{name: "disconnected member", roomID: created.Room.ID, token: created.RoomSessionToken, wantStatus: http.StatusForbidden, wantCode: "member_not_active", wantMessage: "成员不在房间中"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := performAuthorizedRequest(t, router, http.MethodPost, "/v1/rooms/"+tt.roomID+"/livekit-token", tt.token)
			assertHTTPError(t, response, tt.wantStatus, tt.wantCode, tt.wantMessage)
		})
	}
}

func TestFreshLiveKitTokenRejectsExpiredRoom(t *testing.T) {
	router, repository := newCredentialIntegration(t)
	expiredRoom := apiTestRoom("room_expired_credentials", "EXPCRE", domain.RoomStateExpired, nil, credentialNow)
	expiredMember := apiTestMember("mem_expired_credentials", expiredRoom.ID, domain.MemberStateOnline, true, credentialNow)
	if err := repository.CreateRoomWithMember(context.Background(), expiredRoom, expiredMember); err != nil {
		t.Fatalf("CreateRoomWithMember returned error: %v", err)
	}
	token, _, err := session.Sign(session.SignInput{Secret: testCredentialConfig().RoomSessionSecret, RoomID: expiredRoom.ID, MemberID: expiredMember.ID, Now: credentialNow, TTL: 2 * time.Hour})
	if err != nil {
		t.Fatalf("Sign token: %v", err)
	}

	response := performAuthorizedRequest(t, router, http.MethodPost, "/v1/rooms/"+expiredRoom.ID+"/livekit-token", token)

	assertHTTPError(t, response, http.StatusGone, "room_expired", "该房间已过期，请让朋友重新创建")
}

func TestCredentialConfigFailureReturnsInternalErrorBeforeMutation(t *testing.T) {
	creator := &captureContextRoomCreator{}
	createRouter := NewRouter(WithRoomCreator(creator))
	createResponse := performJSONRequest(t, createRouter, http.MethodPost, "/v1/rooms", map[string]string{
		"anonymous_id": "anon_local_123",
		"nickname":     "Alice",
		"avatar_id":    "avatar_07",
	})
	assertHTTPError(t, createResponse, http.StatusInternalServerError, "internal_error", "服务器错误")
	if creator.calls != 0 {
		t.Fatalf("room creator calls = %d, want 0 when credential config is invalid", creator.calls)
	}

	joiner := &captureContextRoomJoiner{}
	joinRouter := NewRouter(WithRoomJoiner(joiner))
	joinResponse := performJSONRequest(t, joinRouter, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  "ABC123",
		"anonymous_id": "anon_local_456",
		"nickname":     "Bob",
		"avatar_id":    "avatar_08",
	})
	assertHTTPError(t, joinResponse, http.StatusInternalServerError, "internal_error", "服务器错误")
	if joiner.calls != 0 {
		t.Fatalf("room joiner calls = %d, want 0 when credential config is invalid", joiner.calls)
	}
}

func assertCredentialFields(t *testing.T, body createRoomResponseBody, roomID string, memberID string) {
	t.Helper()
	cfg := testCredentialConfig()
	if body.LiveKitURL != cfg.LiveKitURL || body.LiveKitToken == "" || body.RoomSessionToken == "" {
		t.Fatalf("credential fields present = url:%t session:%t livekit:%t, want configured url and non-empty tokens", body.LiveKitURL == cfg.LiveKitURL, body.RoomSessionToken != "", body.LiveKitToken != "")
	}
	claims, err := session.Verify(session.VerifyInput{Secret: cfg.RoomSessionSecret, Token: body.RoomSessionToken, Now: credentialNow.Add(time.Minute)})
	if err != nil {
		t.Fatalf("room session token did not verify: %v", err)
	}
	if claims.RoomID != roomID || claims.MemberID != memberID || !claims.ExpiresAt.Equal(credentialNow.Add(2*time.Hour)) {
		t.Fatalf("session claims = %#v, want room/member with 2h expiry", claims)
	}
}

func assertLiveKitTokenScope(t *testing.T, token string, wantRoom string, wantIdentity string, wantName string) {
	t.Helper()
	claims := decodeHTTPJWTPayload(t, token)
	if claims["sub"] != wantIdentity || claims["name"] != wantName {
		t.Fatalf("LiveKit identity/name = %v/%v, want %s/%s", claims["sub"], claims["name"], wantIdentity, wantName)
	}
	video, ok := claims["video"].(map[string]any)
	if !ok {
		t.Fatalf("LiveKit video claim = %#v, want object", claims["video"])
	}
	if video["room"] != wantRoom || video["roomJoin"] != true || video["canPublish"] != true || video["canSubscribe"] != true || video["canPublishData"] != false {
		t.Fatalf("LiveKit video claim = %#v, want scoped room join publish subscribe without data publish", video)
	}
	assertHTTPStringSliceClaim(t, video, "canPublishSources", []string{"microphone"})
}

func createCredentialRoom(t *testing.T, router http.Handler) createRoomResponseBody {
	t.Helper()
	response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms", map[string]string{
		"anonymous_id": "anon_local_123",
		"nickname":     "Alice",
		"avatar_id":    "avatar_07",
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want %d, body_bytes=%d", response.Code, http.StatusCreated, response.Body.Len())
	}
	return decodeCreateRoomResponse(t, response)
}

func newCredentialIntegration(t *testing.T) (http.Handler, *store.Repository) {
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
	return NewRouter(
		WithRoomCreator(roomService),
		WithRoomJoiner(roomService),
		WithRoomMemberAuthorizer(roomService),
		WithCredentialConfig(testCredentialConfig()),
	), repository
}

func testCredentialConfig() CredentialConfig {
	return CredentialConfig{
		LiveKitURL:          "wss://livekit.test",
		LiveKitAPIKey:       "devkey",
		LiveKitAPISecret:    "devsecret",
		RoomSessionSecret:   "room-session-secret",
		RoomSessionTokenTTL: 2 * time.Hour,
		LiveKitTokenTTL:     10 * time.Minute,
		Now:                 func() time.Time { return credentialNow },
	}
}

func performAuthorizedRequest(t *testing.T, handler http.Handler, method string, target string, bearerToken string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, nil)
	if bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

type liveKitTokenResponseBody struct {
	LiveKitURL   string `json:"livekit_url"`
	LiveKitToken string `json:"livekit_token"`
}

func decodeLiveKitTokenResponse(t *testing.T, response *httptest.ResponseRecorder) liveKitTokenResponseBody {
	t.Helper()
	var body liveKitTokenResponseBody
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response returned invalid JSON: %v", err)
	}
	return body
}

func decodeHTTPJWTPayload(t *testing.T, token string) map[string]any {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT parts = %d, want 3", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode JWT payload: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		t.Fatalf("unmarshal JWT payload: %v", err)
	}
	return claims
}

func assertHTTPStringSliceClaim(t *testing.T, claims map[string]any, key string, want []string) {
	t.Helper()
	values, ok := claims[key].([]any)
	if !ok || len(values) != len(want) {
		t.Fatalf("%s claim = %#v, want %v", key, claims[key], want)
	}
	for i, wantValue := range want {
		value, ok := values[i].(string)
		if !ok || value != wantValue {
			t.Fatalf("%s claim = %#v, want %v", key, claims[key], want)
		}
	}
}
