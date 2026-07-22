package identity

import (
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
)

// minCardTokenHMACKeyBytes is the floor for an acceptable card-token HMAC key.
const minCardTokenHMACKeyBytes = 32

// CardTokenHasher provides deterministic keyed hashing for card tokens.
// It uses HMAC-SHA256 so that the same (key, token) pair always produces
// the same hash — enabling lookup without storing the raw token.
type CardTokenHasher struct {
	key []byte
}

// NewCardTokenHasher creates a CardTokenHasher. It rejects keys shorter
// than the minimum to prevent accidentally weak hashing.
func NewCardTokenHasher(key string) (*CardTokenHasher, error) {
	if len([]byte(key)) < minCardTokenHMACKeyBytes {
		return nil, fmt.Errorf("card-token HMAC key must be at least %d bytes, got %d",
			minCardTokenHMACKeyBytes, len([]byte(key)))
	}
	return &CardTokenHasher{key: []byte(key)}, nil
}

// Hash returns the raw HMAC-SHA256 output (32 bytes) for the given token.
// The same (key, token) always produces the same output (deterministic).
// A nil receiver is a programming error; Hash will return ErrMissingHasher
// instead of leaking the raw token.
func (h *CardTokenHasher) Hash(token string) ([]byte, error) {
	if h == nil {
		return nil, ErrMissingHasher
	}
	mac := hmac.New(sha256.New, h.key)
	mac.Write([]byte(token))
	return mac.Sum(nil), nil
}

// ErrMissingHasher is returned when an operation requires a hasher but none
// was provided (e.g., card-token lookup without a configured key).
var ErrMissingHasher = errors.New("card-token hasher is not configured")
