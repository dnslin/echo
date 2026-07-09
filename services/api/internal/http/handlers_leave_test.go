package httpapi

import (
	"context"
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

func TestLeaveRoomReturnsNoContent(t *testing.T) {
	leaver := &captureContextRoomLeaver{}
	router := NewRouter(WithRoomLeaver(leaver))

	response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/room_test/leave", map[string]string{"member_id": "mem_test"})

	if response.Code != http.StatusNoContent {
		t.Fatalf("POST /v1/rooms/{room_id}/leave status = %d, want %d, body: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if response.Body.Len() != 0 {
		t.Fatalf("leave response body = %q, want empty", response.Body.String())
	}
	if leaver.calls != 1 {
		t.Fatalf("room leaver calls = %d, want 1", leaver.calls)
	}
	if leaver.input.RoomID != "room_test" || leaver.input.MemberID != "mem_test" {
		t.Fatalf("leave input = %#v, want room_test/mem_test", leaver.input)
	}
}

func TestLeaveRoomPassesRequestContext(t *testing.T) {
	leaver := &captureContextRoomLeaver{}
	router := NewRouter(WithRoomLeaver(leaver))
	request := httptest.NewRequest(http.MethodPost, "/v1/rooms/room_test/leave", strings.NewReader(`{"member_id":"mem_test"}`))
	request.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(request.Context(), testContextKey{}, "leave-request-context")
	request = request.WithContext(ctx)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("POST /v1/rooms/{room_id}/leave status = %d, want %d, body: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if leaver.contextValue != "leave-request-context" {
		t.Fatalf("leaver context value = %v, want leave-request-context", leaver.contextValue)
	}
}

func TestLeaveRoomRejectsMalformedAndOversizedRequestBeforeService(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{`},
		{name: "oversized", body: `{"member_id":"` + strings.Repeat("a", maxCreateRoomRequestBytes) + `"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaver := &captureContextRoomLeaver{}
			router := NewRouter(WithRoomLeaver(leaver))
			request := httptest.NewRequest(http.MethodPost, "/v1/rooms/room_test/leave", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if leaver.calls != 0 {
				t.Fatalf("room leaver calls = %d, want 0", leaver.calls)
			}
			assertHTTPError(t, response, http.StatusBadRequest, "invalid_request", "请求格式无效")
		})
	}
}

func TestLeaveRoomValidationAndProductErrors(t *testing.T) {
	tests := []struct {
		name        string
		payload     map[string]string
		err         error
		wantStatus  int
		wantCode    string
		wantMessage string
	}{
		{
			name:        "blank member id",
			payload:     map[string]string{"member_id": " "},
			err:         &room.ValidationError{Code: "invalid_member_id", Message: "成员标识不能为空"},
			wantStatus:  http.StatusBadRequest,
			wantCode:    "invalid_member_id",
			wantMessage: "成员标识不能为空",
		},
		{
			name:        "missing room",
			payload:     map[string]string{"member_id": "mem_test"},
			err:         room.ErrRoomNotFound,
			wantStatus:  http.StatusNotFound,
			wantCode:    "room_not_found",
			wantMessage: "房间不存在或已失效",
		},
		{
			name:        "missing member",
			payload:     map[string]string{"member_id": "mem_test"},
			err:         room.ErrMemberNotFound,
			wantStatus:  http.StatusNotFound,
			wantCode:    "member_not_found",
			wantMessage: "成员不在房间中",
		},
		{
			name:        "expired room",
			payload:     map[string]string{"member_id": "mem_test"},
			err:         room.ErrRoomExpired,
			wantStatus:  http.StatusGone,
			wantCode:    "room_expired",
			wantMessage: "该房间已过期，请让朋友重新创建",
		},
		{
			name:        "unexpected error",
			payload:     map[string]string{"member_id": "mem_test"},
			err:         errors.New("store failed"),
			wantStatus:  http.StatusInternalServerError,
			wantCode:    "internal_error",
			wantMessage: "服务器错误",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			leaver := &captureContextRoomLeaver{err: tt.err}
			router := NewRouter(WithRoomLeaver(leaver))
			response := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/room_test/leave", tt.payload)

			assertHTTPError(t, response, tt.wantStatus, tt.wantCode, tt.wantMessage)
		})
	}
}

func TestLeaveRoomIntegrationAllowsJoinBeforeExpiryAndClearsRetention(t *testing.T) {
	router, repository := newLeaveRoomIntegration(t)
	created := createRoomForLeaveIntegration(t, router)

	leaveResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/"+created.Room.ID+"/leave", map[string]string{"member_id": created.Member.ID})
	if leaveResponse.Code != http.StatusNoContent {
		t.Fatalf("POST leave status = %d, want %d, body: %s", leaveResponse.Code, http.StatusNoContent, leaveResponse.Body.String())
	}

	retained, err := repository.FindRoomByInviteCode(context.Background(), created.Room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode retained returned error: %v", err)
	}
	if retained.LastEmptyAt == nil || retained.ExpiresAt == nil {
		t.Fatalf("retained room last_empty_at/expires_at = %v/%v, want both set", retained.LastEmptyAt, retained.ExpiresAt)
	}
	joinResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  created.Room.InviteCode,
		"anonymous_id": "anon_local_456",
		"nickname":     "Alice",
		"avatar_id":    "avatar_08",
	})
	if joinResponse.Code != http.StatusOK {
		t.Fatalf("POST join retained status = %d, want %d, body_bytes=%d", joinResponse.Code, http.StatusOK, joinResponse.Body.Len())
	}
	joined := decodeCreateRoomResponse(t, joinResponse)
	if joined.Room.LastEmptyAt != nil || joined.Room.ExpiresAt != nil {
		t.Fatalf("joined room last_empty_at/expires_at = %v/%v, want nil/nil", joined.Room.LastEmptyAt, joined.Room.ExpiresAt)
	}
}

func TestLeaveRoomIntegrationJoinAfterExpiryReturnsExpired(t *testing.T) {
	router, repository := newLeaveRoomIntegration(t)
	created := createRoomForLeaveIntegration(t, router)
	leaveResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/"+created.Room.ID+"/leave", map[string]string{"member_id": created.Member.ID})
	if leaveResponse.Code != http.StatusNoContent {
		t.Fatalf("POST leave status = %d, want %d, body: %s", leaveResponse.Code, http.StatusNoContent, leaveResponse.Body.String())
	}
	retained, err := repository.FindRoomByInviteCode(context.Background(), created.Room.InviteCode)
	if err != nil {
		t.Fatalf("FindRoomByInviteCode retained returned error: %v", err)
	}
	if retained.ExpiresAt == nil {
		t.Fatalf("retained room expires_at = nil, want due expiry")
	}
	if err := repository.MarkRoomExpired(context.Background(), retained.ID, retained.ExpiresAt.Add(time.Second)); err != nil {
		t.Fatalf("MarkRoomExpired returned error: %v", err)
	}

	joinResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms/join", map[string]string{
		"invite_code":  created.Room.InviteCode,
		"anonymous_id": "anon_local_456",
		"nickname":     "Alice",
		"avatar_id":    "avatar_08",
	})

	assertHTTPError(t, joinResponse, http.StatusGone, "room_expired", "该房间已过期，请让朋友重新创建")
}

func newLeaveRoomIntegration(t *testing.T) (http.Handler, *store.Repository) {
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
	return NewRouter(WithRoomCreator(roomService), WithRoomJoiner(roomService), WithRoomLeaver(roomService), WithCredentialConfig(testCredentialConfig())), repository
}

func createRoomForLeaveIntegration(t *testing.T, router http.Handler) createRoomResponseBody {
	t.Helper()
	createResponse := performJSONRequest(t, router, http.MethodPost, "/v1/rooms", map[string]string{
		"anonymous_id": "anon_local_123",
		"nickname":     "Alice",
		"avatar_id":    "avatar_07",
		"room_name":    "今晚开黑",
	})
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("POST /v1/rooms status = %d, want %d, body_bytes=%d", createResponse.Code, http.StatusCreated, createResponse.Body.Len())
	}
	return decodeCreateRoomResponse(t, createResponse)
}

type captureContextRoomLeaver struct {
	calls        int
	contextValue any
	input        room.LeaveInput
	err          error
}

func (c *captureContextRoomLeaver) LeaveContext(ctx context.Context, input room.LeaveInput) (room.LeaveResult, error) {
	c.calls++
	c.contextValue = ctx.Value(testContextKey{})
	c.input = input
	if c.err != nil {
		return room.LeaveResult{}, c.err
	}
	now := time.Date(2026, 7, 8, 12, 0, 0, 0, time.UTC)
	return room.LeaveResult{
		Room: domain.Room{
			ID:         input.RoomID,
			Name:       "临时房间",
			InviteCode: "ABC123",
			State:      domain.RoomStateActive,
			CreatedAt:  now,
		},
		Member: domain.Member{
			ID:              input.MemberID,
			RoomID:          input.RoomID,
			State:           domain.MemberStateDisconnected,
			VoiceMode:       domain.VoiceModePushToTalk,
			LiveKitIdentity: input.MemberID,
			JoinedAt:        now,
		},
	}, nil
}
