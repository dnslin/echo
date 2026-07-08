package invite

import (
	"strings"
	"testing"
)

func TestGenerateReturnsSixUppercaseAlphanumericCharacters(t *testing.T) {
	generator := NewGenerator()

	code, err := generator.Generate(6)
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if len(code) != 6 {
		t.Fatalf("Generate length = %d, want 6", len(code))
	}
	for _, ch := range code {
		if !strings.ContainsRune(CharSet, ch) {
			t.Fatalf("Generate returned invalid character %q in %q", ch, code)
		}
	}
}

func TestGenerateRejectsInvalidLength(t *testing.T) {
	generator := NewGenerator()

	if _, err := generator.Generate(0); err == nil {
		t.Fatal("Generate(0) error = nil, want error")
	}
}
