package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// dummyPasswordHash ensures unknown-user login attempts still perform one
// bcrypt comparison, reducing username-enumeration signal from response time.
var dummyPasswordHash = func() string {
	hash, err := bcrypt.GenerateFromPassword([]byte("medisync-dummy-password"), bcrypt.DefaultCost)
	if err != nil {
		panic("identity: create dummy password hash: " + err.Error())
	}
	return string(hash)
}()

// Common error returned to callers. It must not distinguish between an
// unknown user and a wrong password so that callers cannot enumerate.
var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInactiveUser       = errors.New("user is inactive")
	ErrMissingCardToken   = errors.New("card token is required")
	ErrMissingUsername    = errors.New("username is required")
	ErrMissingPassword    = errors.New("password is required")
	ErrMissingToken       = errors.New("access token is required")
	// ErrRateLimitExceeded is returned when login attempts exceed the
	// configured rate limit. The same error is used for both
	// per-identifier and per-IP limits so clients cannot distinguish.
	ErrRateLimitExceeded = errors.New("rate limit exceeded")
)

// AuthService handles authentication flows. It depends on the narrow
// UserStore interface rather than the concrete Store, so it can be tested
// with a fake store.
type AuthService struct {
	store  UserStore
	passwd *passwordHelper
	jwt    TokenManager
}

// TokenManager is the narrow JWT interface consumed by AuthService.
type TokenManager interface {
	Issue(user *User) (token string, expiresAt time.Time, err error)
	Parse(tokenString string) (*TokenClaims, error)
}

// passwordHelper exists so the service is unit-testable without importing
// bcrypt directly. In production, identity.HashPassword and
// identity.VerifyPassword are the real implementations.
type passwordHelper struct {
	Hash   func(password string) (string, error)
	Verify func(hash, password string) error
}

// NewAuthService creates an AuthService.
func NewAuthService(store UserStore, jwt TokenManager) *AuthService {
	return &AuthService{
		store: store,
		passwd: &passwordHelper{
			Hash:   HashPassword,
			Verify: VerifyPassword,
		},
		jwt: jwt,
	}
}

// LoginPassword authenticates by username and password. It returns a JWT
// and the user on success, or ErrInvalidCredentials for unknown users,
// wrong passwords, and inactive users (callers must not distinguish).
func (s *AuthService) LoginPassword(ctx context.Context, username, password string) (string, time.Time, *User, error) {
	if username == "" {
		return "", time.Time{}, nil, ErrMissingUsername
	}
	if password == "" {
		return "", time.Time{}, nil, ErrMissingPassword
	}

	user, err := s.store.GetByUsername(ctx, username)
	if err != nil {
		return "", time.Time{}, nil, fmt.Errorf("lookup user: %w", err)
	}

	passwordHash := dummyPasswordHash
	if user != nil {
		passwordHash = user.PasswordHash
	}
	if err := s.passwd.Verify(passwordHash, password); user == nil || err != nil {
		return "", time.Time{}, nil, ErrInvalidCredentials
	}

	if !user.Active {
		return "", time.Time{}, nil, ErrInactiveUser
	}

	token, expiresAt, err := s.jwt.Issue(user)
	if err != nil {
		return "", time.Time{}, nil, fmt.Errorf("issue token: %w", err)
	}
	return token, expiresAt, user, nil
}

// LoginCard authenticates by card token. It returns a JWT and the user on
// success, or ErrInvalidCredentials if the token is unknown or the user is
// inactive.
func (s *AuthService) LoginCard(ctx context.Context, cardToken string) (string, time.Time, *User, error) {
	if cardToken == "" {
		return "", time.Time{}, nil, ErrMissingCardToken
	}

	user, err := s.store.GetByCardToken(ctx, cardToken)
	if err != nil {
		return "", time.Time{}, nil, fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return "", time.Time{}, nil, ErrInvalidCredentials
	}

	if !user.Active {
		return "", time.Time{}, nil, ErrInactiveUser
	}

	token, expiresAt, err := s.jwt.Issue(user)
	if err != nil {
		return "", time.Time{}, nil, fmt.Errorf("issue token: %w", err)
	}
	return token, expiresAt, user, nil
}

// WhoAmI resolves the current user from a JWT token string. It returns the
// user or an error when the token is missing, invalid, or the user is not
// found or inactive.
func (s *AuthService) WhoAmI(ctx context.Context, tokenString string) (*User, error) {
	if tokenString == "" {
		return nil, ErrMissingToken
	}

	claims, err := s.jwt.Parse(tokenString)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if claims.Subject == "" {
		return nil, ErrInvalidCredentials
	}

	user, err := s.store.GetByID(ctx, claims.Subject)
	if err != nil {
		return nil, fmt.Errorf("lookup user: %w", err)
	}
	if user == nil {
		return nil, ErrInvalidCredentials
	}
	if !user.Active {
		return nil, ErrInactiveUser
	}

	return user, nil
}
