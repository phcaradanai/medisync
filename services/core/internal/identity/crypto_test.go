package identity

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewCardTokenHasherValidKey(t *testing.T) {
	key := "a-32-byte-card-token-hmac-key!!!"
	h, err := NewCardTokenHasher(key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil hasher")
	}
	if h.key == nil {
		t.Fatal("key should be set")
	}
}

func TestNewCardTokenHasherTooShort(t *testing.T) {
	_, err := NewCardTokenHasher("short")
	if err == nil {
		t.Fatal("expected error for too-short key")
	}
	if !strings.Contains(err.Error(), "at least") {
		t.Errorf("error should mention minimum length: %v", err)
	}
}

func TestNewCardTokenHasherExactMinLength(t *testing.T) {
	key := strings.Repeat("a", minCardTokenHMACKeyBytes)
	h, err := NewCardTokenHasher(key)
	if err != nil {
		t.Fatalf("unexpected error for exactly-minimum key: %v", err)
	}
	if h == nil {
		t.Fatal("expected non-nil hasher")
	}
}

func TestCardTokenHasherDeterministic(t *testing.T) {
	h, err := NewCardTokenHasher("this-is-a-32-byte-card-token-key!!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	token := "card-token-abc-123"
	h1, _ := h.Hash(token)
	h2, _ := h.Hash(token)
	if !bytes.Equal(h1, h2) {
		t.Errorf("same (key,token) should produce same hash: %x vs %x", h1, h2)
	}
}

func TestCardTokenHasherDifferentTokensProduceDifferentHashes(t *testing.T) {
	h, err := NewCardTokenHasher("card-token-hmac-key-minimum-32!!")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h1, _ := h.Hash("token-a")
	h2, _ := h.Hash("token-b")
	if bytes.Equal(h1, h2) {
		t.Error("different tokens should produce different hashes")
	}
}

func TestCardTokenHasherDifferentKeysProduceDifferentHashes(t *testing.T) {
	h1, _ := NewCardTokenHasher("key-one-minimum-32-bytes-key-one")
	h2, _ := NewCardTokenHasher("key-two-minimum-32-bytes-key-two")

	token := "same-token"
	a, _ := h1.Hash(token)
	b, _ := h2.Hash(token)
	if bytes.Equal(a, b) {
		t.Error("different keys should produce different hashes for the same token")
	}
}

func TestCardTokenHasherOutputLength(t *testing.T) {
	h, _ := NewCardTokenHasher("a-valid-card-token-hmac-key-32!!")
	result, _ := h.Hash("test-token")

	// HMAC-SHA256 produces 32 bytes
	if len(result) != 32 {
		t.Errorf("expected 32 bytes, got %d: %x", len(result), result)
	}
}

func TestCardTokenHasherNilReturnsError(t *testing.T) {
	var h *CardTokenHasher
	_, err := h.Hash("test")
	if err != ErrMissingHasher {
		t.Errorf("nil hasher should return ErrMissingHasher, got %v", err)
	}
}

func TestCardTokenHasherNonEmptyHash(t *testing.T) {
	h, _ := NewCardTokenHasher("minimum-32-byte-card-token-hmac-key")
	result, _ := h.Hash("anything")
	if len(result) == 0 {
		t.Error("hash should not be empty")
	}
	if bytes.Equal(result, []byte("anything")) {
		t.Error("hash should differ from raw input token")
	}
}
