package invite

import (
	"errors"
	"testing"
)

func TestNormalizeCanonicalizesCaseWhitespaceAndHyphen(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "lowercase", input: "k7m9q2"},
		{name: "spaces and hyphen", input: " k7-m9 q2 "},
		{name: "mixed whitespace", input: "\tk7 - m9\nq2\r"},
		{name: "already canonical", input: "K7M9Q2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, err := Normalize(tt.input)
			if err != nil {
				t.Fatalf("Normalize returned error: %v", err)
			}
			if code != "K7M9Q2" {
				t.Fatalf("Normalize(%q) = %q, want K7M9Q2", tt.input, code)
			}
		})
	}
}

func TestNormalizeRejectsEmptyAfterIgnoredCharacters(t *testing.T) {
	tests := []string{"", "   ", " - - \t\n"}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := Normalize(input)
			if !errors.Is(err, ErrEmptyCode) {
				t.Fatalf("Normalize(%q) error = %v, want ErrEmptyCode", input, err)
			}
		})
	}
}

func TestNormalizeRejectsInvalidFormat(t *testing.T) {
	tests := []string{
		"ABC12",
		"ABC1234",
		"ABC12!",
		"你ABC12",
		"ABC_12",
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			_, err := Normalize(input)
			if !errors.Is(err, ErrInvalidFormat) {
				t.Fatalf("Normalize(%q) error = %v, want ErrInvalidFormat", input, err)
			}
		})
	}
}
