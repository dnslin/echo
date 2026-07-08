package invite

import (
	"crypto/rand"
	"errors"
	"io"
	"math/big"
	"unicode"
)

const CharSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var (
	ErrInvalidLength = errors.New("invite code length must be positive")
	ErrEmptyCode     = errors.New("invite code is empty")
	ErrInvalidFormat = errors.New("invite code format is invalid")
)

func Normalize(input string) (string, error) {
	code := make([]rune, 0, 6)
	for _, ch := range input {
		if unicode.IsSpace(ch) || ch == '-' {
			continue
		}
		if ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		if !isAllowedCodeRune(ch) {
			return "", ErrInvalidFormat
		}
		code = append(code, ch)
	}
	if len(code) == 0 {
		return "", ErrEmptyCode
	}
	if len(code) != 6 {
		return "", ErrInvalidFormat
	}
	return string(code), nil
}

func isAllowedCodeRune(ch rune) bool {
	return (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

type Generator struct {
	reader io.Reader
}

func NewGenerator() Generator {
	return Generator{reader: rand.Reader}
}

func (g Generator) Generate(length int) (string, error) {
	if length <= 0 {
		return "", ErrInvalidLength
	}

	reader := g.reader
	if reader == nil {
		reader = rand.Reader
	}
	code := make([]byte, length)
	limit := big.NewInt(int64(len(CharSet)))
	for i := range code {
		index, err := rand.Int(reader, limit)
		if err != nil {
			return "", err
		}
		code[i] = CharSet[index.Int64()]
	}
	return string(code), nil
}
