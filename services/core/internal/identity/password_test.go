package identity

import (
	"strings"
	"testing"
)

func TestHashPasswordSuccess(t *testing.T) {
	hash, err := HashPassword("my-secure-password")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
	if !strings.HasPrefix(hash, "$2a$") && !strings.HasPrefix(hash, "$2b$") && !strings.HasPrefix(hash, "$2y$") {
		t.Errorf("expected bcrypt hash prefix, got %q", hash[:min(len(hash), 5)])
	}
}

func TestHashPasswordBlank(t *testing.T) {
	tests := []string{"", "   ", "\t", "\n"}
	for _, pw := range tests {
		t.Run("blank", func(t *testing.T) {
			_, err := HashPassword(pw)
			if err == nil {
				t.Fatalf("expected error for blank password %q", pw)
			}
			if !strings.Contains(err.Error(), "invalid credentials") {
				t.Errorf("error should be generic: %v", err)
			}
		})
	}
}

func TestHashPasswordOver72Bytes(t *testing.T) {
	// Build a password that is exactly 73 bytes.
	long := strings.Repeat("a", 73)
	_, err := HashPassword(long)
	if err == nil {
		t.Fatal("expected error for password over 72 bytes")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("error should be generic: %v", err)
	}
}

func TestHashPasswordExactly72Bytes(t *testing.T) {
	// 72 bytes is the bcrypt limit — should succeed.
	exact := strings.Repeat("a", 72)
	hash, err := HashPassword(exact)
	if err != nil {
		t.Fatalf("unexpected error for 72-byte password: %v", err)
	}
	if hash == "" {
		t.Fatal("expected non-empty hash")
	}
}

func TestVerifyPasswordSuccess(t *testing.T) {
	hash, err := HashPassword("correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	err = VerifyPassword(hash, "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestVerifyPasswordWrongPassword(t *testing.T) {
	hash, err := HashPassword("correct")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	err = VerifyPassword(hash, "wrong")
	if err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestVerifyPasswordBlank(t *testing.T) {
	hash, _ := HashPassword("valid")
	err := VerifyPassword(hash, "")
	if err == nil {
		t.Fatal("expected error for blank password")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("error should be generic: %v", err)
	}
}

func TestVerifyPasswordOver72Bytes(t *testing.T) {
	hash, _ := HashPassword("valid")
	long := strings.Repeat("w", 73)
	err := VerifyPassword(hash, long)
	if err == nil {
		t.Fatal("expected error for password over 72 bytes")
	}
	if !strings.Contains(err.Error(), "invalid credentials") {
		t.Errorf("error should be generic: %v", err)
	}
}

func TestVerifyPasswordWrongHash(t *testing.T) {
	err := VerifyPassword("not-a-valid-hash", "password")
	if err == nil {
		t.Fatal("expected bcrypt error for invalid hash")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
