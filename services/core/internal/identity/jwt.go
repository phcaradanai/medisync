package identity

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Clock abstracts time for deterministic tests.
type Clock interface {
	Now() time.Time
}

// realClock uses the host clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// TokenClaims carries the identity JWT payload.
type TokenClaims struct {
	jwt.RegisteredClaims
	Role    string   `json:"role"`
	WardIDs []string `json:"ward_ids"`
}

// KioskTokenClaims carries the kiosk JWT payload.
type KioskTokenClaims struct {
	jwt.RegisteredClaims
	Code        string `json:"code"`
	DisplayName string `json:"display_name"`
}

// JWTManager issues and validates HS256 JWTs. All operations reject
// unexpected signing algorithms, malformed tokens, and expired tokens.
type JWTManager struct {
	secret []byte
	ttl    time.Duration
	clock  Clock
}

// NewJWTManager creates a JWTManager. It rejects secrets shorter than 32
// bytes and non-positive TTLs.
func NewJWTManager(secret string, ttl time.Duration, clock Clock) (*JWTManager, error) {
	if len([]byte(secret)) < 32 {
		return nil, errors.New("jwt secret must be at least 32 bytes")
	}
	if ttl <= 0 {
		return nil, errors.New("jwt ttl must be positive")
	}
	if clock == nil {
		clock = realClock{}
	}
	return &JWTManager{
		secret: []byte(secret),
		ttl:    ttl,
		clock:  clock,
	}, nil
}

// Issue creates a signed HS256 token for the given user.
func (m *JWTManager) Issue(user *User) (string, time.Time, error) {
	now := m.clock.Now()
	expiresAt := now.Add(m.ttl)

	claims := TokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		Role:    string(user.Role),
		WardIDs: user.WardIDs,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expiresAt, nil
}

// Parse validates a token string and returns its claims. It rejects:
//   - tokens signed with any algorithm other than HS256;
//   - malformed, expired, or otherwise invalid tokens.
func (m *JWTManager) Parse(tokenString string) (*TokenClaims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&TokenClaims{},
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(0),
		jwt.WithTimeFunc(func() time.Time { return m.clock.Now() }),
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*TokenClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}

	return claims, nil
}

// IssueKiosk creates a signed HS256 token for the given kiosk.
func (m *JWTManager) IssueKiosk(k *Kiosk) (string, time.Time, error) {
	now := m.clock.Now()
	expiresAt := now.Add(m.ttl)

	claims := KioskTokenClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   k.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
		Code:        k.Code,
		DisplayName: k.DisplayName,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign kiosk token: %w", err)
	}
	return signed, expiresAt, nil
}

// ParseKiosk validates a kiosk token string and returns its claims.
// It applies the same security rules as Parse (HS256 only, expiry
// required, zero leeway).
func (m *JWTManager) ParseKiosk(tokenString string) (*KioskTokenClaims, error) {
	token, err := jwt.ParseWithClaims(
		tokenString,
		&KioskTokenClaims{},
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(0),
		jwt.WithTimeFunc(func() time.Time { return m.clock.Now() }),
	)
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*KioskTokenClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid kiosk token")
	}

	return claims, nil
}
