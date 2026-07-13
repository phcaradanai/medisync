package identity

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// bcryptMaxBytes is the maximum password length bcrypt accepts.
const bcryptMaxBytes = 72

// HashPassword returns a bcrypt hash of the given password.
// It rejects blank input and passwords longer than 72 bytes.
func HashPassword(password string) (string, error) {
	if err := checkPasswordBytes(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword returns nil when the given password matches the bcrypt hash.
// It rejects blank input and passwords longer than 72 bytes before hashing.
func VerifyPassword(hash, password string) error {
	if err := checkPasswordBytes(password); err != nil {
		return err
	}
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err != nil {
		return err
	}
	return nil
}

// checkPasswordBytes validates that password is not blank and does not exceed
// bcrypt's 72-byte limit. The error must not discriminate between reasons.
func checkPasswordBytes(password string) error {
	if trimmed := strings.TrimSpace(password); trimmed == "" {
		return errors.New("invalid credentials")
	}
	if len([]byte(password)) > bcryptMaxBytes {
		return errors.New("invalid credentials")
	}
	return nil
}
