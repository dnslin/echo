package session

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const CurrentVersion = 1

const DefaultTTL = 2 * time.Hour

var (
	ErrInvalidToken = errors.New("invalid room session token")
	ErrExpiredToken = errors.New("expired room session token")
)

type Claims struct {
	Version   int       `json:"version"`
	RoomID    string    `json:"room_id"`
	MemberID  string    `json:"member_id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type SignInput struct {
	Secret   string
	RoomID   string
	MemberID string
	Now      time.Time
	TTL      time.Duration
}

type VerifyInput struct {
	Secret string
	Token  string
	Now    time.Time
}

func Sign(input SignInput) (string, Claims, error) {
	if strings.TrimSpace(input.Secret) == "" || strings.TrimSpace(input.RoomID) == "" || strings.TrimSpace(input.MemberID) == "" || input.TTL <= 0 {
		return "", Claims{}, ErrInvalidToken
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	claims := Claims{
		Version:   CurrentVersion,
		RoomID:    strings.TrimSpace(input.RoomID),
		MemberID:  strings.TrimSpace(input.MemberID),
		ExpiresAt: now.Add(input.TTL),
	}
	token, err := SignClaims(input.Secret, claims)
	if err != nil {
		return "", Claims{}, err
	}
	return token, claims, nil
}

func SignClaims(secret string, claims Claims) (string, error) {
	if strings.TrimSpace(secret) == "" {
		return "", ErrInvalidToken
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payloadSegment := base64.RawURLEncoding.EncodeToString(payload)
	signature := signPayload(secret, payloadSegment)
	return payloadSegment + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func Verify(input VerifyInput) (Claims, error) {
	if strings.TrimSpace(input.Secret) == "" || strings.TrimSpace(input.Token) == "" {
		return Claims{}, ErrInvalidToken
	}
	parts := strings.Split(input.Token, ".")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Claims{}, ErrInvalidToken
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	expected := signPayload(input.Secret, parts[0])
	if subtle.ConstantTimeEq(int32(len(signature)), int32(len(expected))) != 1 || hmac.Equal(signature, expected) == false {
		return Claims{}, ErrInvalidToken
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Claims{}, ErrInvalidToken
	}
	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return Claims{}, ErrInvalidToken
	}
	if claims.Version != CurrentVersion || strings.TrimSpace(claims.RoomID) == "" || strings.TrimSpace(claims.MemberID) == "" || claims.ExpiresAt.IsZero() {
		return Claims{}, ErrInvalidToken
	}
	now := input.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !claims.ExpiresAt.After(now) {
		return Claims{}, ErrExpiredToken
	}
	claims.RoomID = strings.TrimSpace(claims.RoomID)
	claims.MemberID = strings.TrimSpace(claims.MemberID)
	claims.ExpiresAt = claims.ExpiresAt.UTC()
	return claims, nil
}

func signPayload(secret string, payloadSegment string) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payloadSegment))
	return mac.Sum(nil)
}
