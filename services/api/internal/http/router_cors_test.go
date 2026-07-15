package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAllowsConfiguredDevelopmentOriginPreflight(t *testing.T) {
	router := NewRouter(WithCORSOriginPatterns([]string{"http://127.0.0.1:*"}))
	request := httptest.NewRequest(http.MethodOptions, "/v1/rooms", nil)
	request.Header.Set("Origin", "http://127.0.0.1:9245")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Content-Type")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /v1/rooms status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:9245" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want requesting development origin", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Methods"); got != "POST, OPTIONS" {
		t.Fatalf("Access-Control-Allow-Methods = %q, want %q", got, "POST, OPTIONS")
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want %q", got, "Content-Type, Authorization")
	}
	if got := response.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary = %q, want %q", got, "Origin")
	}
}

func TestCORSAllowsAuthorizedRoomRequestPreflight(t *testing.T) {
	router := NewRouter(WithCORSOriginPatterns([]string{"http://127.0.0.1:*"}))
	request := httptest.NewRequest(http.MethodOptions, "/v1/rooms/room-1/livekit-token", nil)
	request.Header.Set("Origin", "http://127.0.0.1:9245")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /v1/rooms/room-1/livekit-token status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:9245" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want requesting development origin", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Headers"); got != "Content-Type, Authorization" {
		t.Fatalf("Access-Control-Allow-Headers = %q, want %q", got, "Content-Type, Authorization")
	}
}

func TestCORSRejectsUnknownOrigin(t *testing.T) {
	router := NewRouter(WithCORSOriginPatterns([]string{"http://127.0.0.1:*"}))
	request := httptest.NewRequest(http.MethodOptions, "/v1/rooms", nil)
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Authorization, Content-Type")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("OPTIONS /v1/rooms from unknown origin status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want no cross-origin permission", got)
	}
}

func TestCORSAllowsConfiguredWailsOrigin(t *testing.T) {
	router := NewRouter(WithCORSOriginPatterns([]string{"wails://wails.localhost"}))
	request := httptest.NewRequest(http.MethodOptions, "/v1/rooms", nil)
	request.Header.Set("Origin", "wails://wails.localhost")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Content-Type")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /v1/rooms status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "wails://wails.localhost" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want configured Wails origin", got)
	}
}

func TestCORSRejectsUnsupportedPreflightRequirements(t *testing.T) {
	testCases := []struct {
		name    string
		method  string
		headers string
	}{
		{name: "unsupported method", method: http.MethodPatch, headers: "Content-Type"},
		{name: "unsupported request header", method: http.MethodPost, headers: "Content-Type, X-Requested-With"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			router := NewRouter(WithCORSOriginPatterns([]string{"http://127.0.0.1:*"}))
			request := httptest.NewRequest(http.MethodOptions, "/v1/rooms", nil)
			request.Header.Set("Origin", "http://127.0.0.1:9245")
			request.Header.Set("Access-Control-Request-Method", testCase.method)
			request.Header.Set("Access-Control-Request-Headers", testCase.headers)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			if response.Code != http.StatusForbidden {
				t.Fatalf("OPTIONS /v1/rooms status = %d, want %d", response.Code, http.StatusForbidden)
			}
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
				t.Fatalf("Access-Control-Allow-Origin = %q, want no cross-origin permission", got)
			}
		})
	}
}

func TestCORSDoesNotAcceptWildcardOriginPattern(t *testing.T) {
	router := NewRouter(WithCORSOriginPatterns([]string{"*"}))
	request := httptest.NewRequest(http.MethodOptions, "/v1/rooms", nil)
	request.Header.Set("Origin", "https://untrusted.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	request.Header.Set("Access-Control-Request-Headers", "Content-Type")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("OPTIONS /v1/rooms with wildcard pattern status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want no wildcard permission", got)
	}
}
