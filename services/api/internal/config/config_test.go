package config

import (
	"testing"
	"time"
)

func TestDefaultCredentialTTLsAreExplicit(t *testing.T) {
	cfg := Default()
	if cfg.RoomSessionTokenTTL != 2*time.Hour {
		t.Fatalf("RoomSessionTokenTTL = %v, want 2h", cfg.RoomSessionTokenTTL)
	}
	if cfg.LiveKitTokenTTL != 10*time.Minute {
		t.Fatalf("LiveKitTokenTTL = %v, want 10m", cfg.LiveKitTokenTTL)
	}
}
