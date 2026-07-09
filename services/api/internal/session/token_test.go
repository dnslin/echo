package session

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

var tokenNow = time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)

func TestSignAndVerifyRoomSessionToken(t *testing.T) {
	token, claims, err := Sign(SignInput{
		Secret:   "room-session-secret",
		RoomID:   "room_abc",
		MemberID: "mem_abc",
		Now:      tokenNow,
		TTL:      DefaultTTL,
	})
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}
	if token == "" || strings.Contains(token, "room_abc") || strings.Contains(token, "mem_abc") {
		t.Fatalf("token should be non-empty and URL-safe encoded, got %q", token)
	}
	if claims.RoomID != "room_abc" || claims.MemberID != "mem_abc" || claims.Version != CurrentVersion {
		t.Fatalf("signed claims = %#v, want room/member/version", claims)
	}
	if !claims.ExpiresAt.Equal(tokenNow.Add(2 * time.Hour)) {
		t.Fatalf("signed expiry = %v, want %v", claims.ExpiresAt, tokenNow.Add(2*time.Hour))
	}

	verified, err := Verify(VerifyInput{Secret: "room-session-secret", Token: token, Now: tokenNow.Add(time.Minute)})
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if verified.RoomID != claims.RoomID || verified.MemberID != claims.MemberID || !verified.ExpiresAt.Equal(claims.ExpiresAt) {
		t.Fatalf("verified claims = %#v, want %#v", verified, claims)
	}
}

func TestVerifyRejectsExpiredRoomSessionToken(t *testing.T) {
	token, _, err := Sign(SignInput{Secret: "room-session-secret", RoomID: "room_abc", MemberID: "mem_abc", Now: tokenNow, TTL: DefaultTTL})
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}

	_, err = Verify(VerifyInput{Secret: "room-session-secret", Token: token, Now: tokenNow.Add(DefaultTTL)})
	if !errors.Is(err, ErrExpiredToken) {
		t.Fatalf("Verify expired error = %v, want ErrExpiredToken", err)
	}
}

func TestVerifyRejectsInvalidRoomSessionTokens(t *testing.T) {
	validToken, _, err := Sign(SignInput{Secret: "room-session-secret", RoomID: "room_abc", MemberID: "mem_abc", Now: tokenNow, TTL: DefaultTTL})
	if err != nil {
		t.Fatalf("Sign returned error: %v", err)
	}

	tests := []struct {
		name   string
		token  string
		secret string
	}{
		{name: "tampered payload", token: "A" + validToken[1:], secret: "room-session-secret"},
		{name: "wrong secret", token: validToken, secret: "different-secret"},
		{name: "malformed", token: "not-a-token", secret: "room-session-secret"},
		{name: "invalid payload base64", token: signedTokenForPayloadSegment("room-session-secret", "%%%"), secret: "room-session-secret"},
		{name: "invalid json payload", token: signedTokenForPayloadBytes("room-session-secret", []byte("not-json")), secret: "room-session-secret"},
		{name: "missing secret", token: validToken, secret: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Verify(VerifyInput{Secret: tt.secret, Token: tt.token, Now: tokenNow})
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestVerifyRejectsMissingClaimsAndUnsupportedVersion(t *testing.T) {
	tests := []struct {
		name   string
		claims Claims
	}{
		{name: "missing room", claims: Claims{Version: CurrentVersion, MemberID: "mem_abc", ExpiresAt: tokenNow.Add(time.Hour)}},
		{name: "missing member", claims: Claims{Version: CurrentVersion, RoomID: "room_abc", ExpiresAt: tokenNow.Add(time.Hour)}},
		{name: "missing expiry", claims: Claims{Version: CurrentVersion, RoomID: "room_abc", MemberID: "mem_abc"}},
		{name: "unsupported version", claims: Claims{Version: CurrentVersion + 1, RoomID: "room_abc", MemberID: "mem_abc", ExpiresAt: tokenNow.Add(time.Hour)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := SignClaims("room-session-secret", tt.claims)
			if err != nil {
				t.Fatalf("SignClaims returned error: %v", err)
			}
			_, err = Verify(VerifyInput{Secret: "room-session-secret", Token: token, Now: tokenNow})
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Verify error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func TestSignRejectsInvalidRoomSessionInputs(t *testing.T) {
	tests := []struct {
		name  string
		input SignInput
	}{
		{name: "blank secret", input: SignInput{Secret: " ", RoomID: "room_abc", MemberID: "mem_abc", Now: tokenNow, TTL: DefaultTTL}},
		{name: "blank room", input: SignInput{Secret: "room-session-secret", RoomID: " ", MemberID: "mem_abc", Now: tokenNow, TTL: DefaultTTL}},
		{name: "blank member", input: SignInput{Secret: "room-session-secret", RoomID: "room_abc", MemberID: " ", Now: tokenNow, TTL: DefaultTTL}},
		{name: "non-positive ttl", input: SignInput{Secret: "room-session-secret", RoomID: "room_abc", MemberID: "mem_abc", Now: tokenNow, TTL: 0}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := Sign(tt.input)
			if !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("Sign error = %v, want ErrInvalidToken", err)
			}
		})
	}
}

func signedTokenForPayloadBytes(secret string, payload []byte) string {
	return signedTokenForPayloadSegment(secret, base64.RawURLEncoding.EncodeToString(payload))
}

func signedTokenForPayloadSegment(secret string, payloadSegment string) string {
	signature := signPayload(secret, payloadSegment)
	return payloadSegment + "." + base64.RawURLEncoding.EncodeToString(signature)
}
