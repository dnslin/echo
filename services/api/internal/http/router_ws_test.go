package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRoomWebSocketRouteIsRegisteredOnlyWhenConfigured(t *testing.T) {
	unconfigured := NewRouter()
	missingRequest := httptest.NewRequest(http.MethodGet, "/v1/rooms/room_test/ws", nil)
	missingResponse := httptest.NewRecorder()
	unconfigured.ServeHTTP(missingResponse, missingRequest)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("unconfigured websocket route status = %d, want 404", missingResponse.Code)
	}

	websocketHandler := &captureRoomWebSocket{}
	configured := NewRouter(WithRoomWebSocket(websocketHandler))
	request := httptest.NewRequest(http.MethodGet, "/v1/rooms/room_test/ws?token=real-sensitive-token&other=kept", nil)
	response := httptest.NewRecorder()
	configured.ServeHTTP(response, request)

	if response.Code != http.StatusTeapot {
		t.Fatalf("configured websocket route status = %d, want 418", response.Code)
	}
	if websocketHandler.roomID != "room_test" {
		t.Fatalf("websocket handler roomID = %q, want room_test", websocketHandler.roomID)
	}
	if websocketHandler.token != "real-sensitive-token" {
		t.Fatalf("websocket handler token = %q, want original token", websocketHandler.token)
	}
}

func TestRedactedTokenRequestPreservesOriginalAndOtherQuery(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/v1/rooms/room_test/ws?token=real-sensitive-token&other=kept", nil)

	redacted := redactedTokenRequest(request)

	if request.URL.Query().Get("token") != "real-sensitive-token" {
		t.Fatalf("original request token = %q, want unchanged original token", request.URL.Query().Get("token"))
	}
	query := redacted.URL.Query()
	if query.Get("token") != "redacted" || query.Get("other") != "kept" {
		t.Fatalf("redacted query token/other = %q/%q, want redacted/kept", query.Get("token"), query.Get("other"))
	}
	if strings.Contains(redacted.RequestURI, "real-sensitive-token") || !strings.Contains(redacted.RequestURI, "token=redacted") || !strings.Contains(redacted.RequestURI, "other=kept") {
		t.Fatalf("redacted RequestURI = %q, want token redacted and other query preserved", redacted.RequestURI)
	}
}

type captureRoomWebSocket struct {
	roomID string
	token  string
}

func (c *captureRoomWebSocket) ServeRoomHTTP(w http.ResponseWriter, r *http.Request, roomID string) {
	c.roomID = roomID
	c.token = r.URL.Query().Get("token")
	w.WriteHeader(http.StatusTeapot)
}
