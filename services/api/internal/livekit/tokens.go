package livekit

import (
	"errors"
	"strings"
	"time"

	"github.com/livekit/protocol/auth"
)

const DefaultTokenTTL = 10 * time.Minute

var ErrInvalidInput = errors.New("invalid livekit token input")

type JoinTokenInput struct {
	APIKey    string
	APISecret string
	RoomName  string
	Identity  string
	Name      string
	ValidFor  time.Duration
}

func JoinToken(input JoinTokenInput) (string, error) {
	apiKey := strings.TrimSpace(input.APIKey)
	apiSecret := strings.TrimSpace(input.APISecret)
	roomName := strings.TrimSpace(input.RoomName)
	identity := strings.TrimSpace(input.Identity)
	if apiKey == "" || apiSecret == "" || roomName == "" || identity == "" || input.ValidFor <= 0 {
		return "", ErrInvalidInput
	}

	canPublish := true
	canSubscribe := true
	token := auth.NewAccessToken(apiKey, apiSecret).
		SetIdentity(identity).
		SetName(strings.TrimSpace(input.Name)).
		SetValidFor(input.ValidFor).
		SetVideoGrant(&auth.VideoGrant{
			RoomJoin:     true,
			Room:         roomName,
			CanPublish:   &canPublish,
			CanSubscribe: &canSubscribe,
		})
	return token.ToJWT()
}
