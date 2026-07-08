package invite

import (
	"crypto/rand"
	"errors"
	"io"
	"math/big"
)

const CharSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var ErrInvalidLength = errors.New("invite code length must be positive")

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
