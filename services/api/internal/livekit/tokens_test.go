package livekit

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestJoinTokenScopesParticipantToRoom(t *testing.T) {
	before := time.Now().UTC()
	token, err := JoinToken(JoinTokenInput{
		APIKey:    "devkey",
		APISecret: "devsecret",
		RoomName:  "lk_room_abc",
		Identity:  "mem_abc",
		Name:      "Alice",
		ValidFor:  DefaultTokenTTL,
	})
	after := time.Now().UTC()
	if err != nil {
		t.Fatalf("JoinToken returned error: %v", err)
	}
	if token == "" || strings.Contains(token, "devsecret") {
		t.Fatalf("token should be non-empty and must not contain secret plaintext")
	}

	claims := decodeJWTPayload(t, token)
	if claims["iss"] != "devkey" || claims["sub"] != "mem_abc" || claims["name"] != "Alice" {
		t.Fatalf("issuer/identity/name claims = %v/%v/%v, want devkey/mem_abc/Alice", claims["iss"], claims["sub"], claims["name"])
	}
	video := claimObject(t, claims, "video")
	assertBoolClaim(t, video, "roomJoin", true)
	assertStringClaim(t, video, "room", "lk_room_abc")
	assertBoolClaim(t, video, "canPublish", true)
	assertBoolClaim(t, video, "canSubscribe", true)
	assertBoolClaim(t, video, "canPublishData", false)
	assertStringSliceClaim(t, video, "canPublishSources", []string{"microphone"})
	assertAbsentClaims(t, video, "roomAdmin", "roomCreate", "roomList", "roomRecord", "roomMove", "ingressAdmin", "sipCall")
	assertAbsentClaims(t, claims, "sip", "agent", "agentDispatch")

	expUnix, ok := claims["exp"].(float64)
	if !ok {
		t.Fatalf("exp claim = %#v, want unix timestamp", claims["exp"])
	}
	expiresAt := time.Unix(int64(expUnix), 0).UTC()
	if expiresAt.Before(before.Add(DefaultTokenTTL-2*time.Second)) || expiresAt.After(after.Add(DefaultTokenTTL+2*time.Second)) {
		t.Fatalf("exp = %v, want approximately now + %v", expiresAt, DefaultTokenTTL)
	}
}

func TestJoinTokenRejectsInvalidInputs(t *testing.T) {
	valid := JoinTokenInput{APIKey: "devkey", APISecret: "devsecret", RoomName: "lk_room_abc", Identity: "mem_abc", ValidFor: DefaultTokenTTL}
	tests := []struct {
		name   string
		mutate func(*JoinTokenInput)
	}{
		{name: "blank api key", mutate: func(input *JoinTokenInput) { input.APIKey = " " }},
		{name: "blank api secret", mutate: func(input *JoinTokenInput) { input.APISecret = " " }},
		{name: "blank room", mutate: func(input *JoinTokenInput) { input.RoomName = " " }},
		{name: "blank identity", mutate: func(input *JoinTokenInput) { input.Identity = " " }},
		{name: "non-positive ttl", mutate: func(input *JoinTokenInput) { input.ValidFor = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := valid
			tt.mutate(&input)
			_, err := JoinToken(input)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("JoinToken error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func decodeJWTPayload(t *testing.T, token string) map[string]any {
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

func claimObject(t *testing.T, claims map[string]any, key string) map[string]any {
	t.Helper()
	value, ok := claims[key].(map[string]any)
	if !ok {
		t.Fatalf("%s claim = %#v, want object", key, claims[key])
	}
	return value
}

func assertBoolClaim(t *testing.T, claims map[string]any, key string, want bool) {
	t.Helper()
	value, ok := claims[key].(bool)
	if !ok || value != want {
		t.Fatalf("%s claim = %#v, want %v", key, claims[key], want)
	}
}

func assertStringClaim(t *testing.T, claims map[string]any, key string, want string) {
	t.Helper()
	value, ok := claims[key].(string)
	if !ok || value != want {
		t.Fatalf("%s claim = %#v, want %q", key, claims[key], want)
	}
}

func assertStringSliceClaim(t *testing.T, claims map[string]any, key string, want []string) {
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

func assertAbsentClaims(t *testing.T, claims map[string]any, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, ok := claims[key]; ok {
			t.Fatalf("%s claim is present in %#v, want absent", key, claims)
		}
	}
}
