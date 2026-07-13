package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// fixedClock returns a constant time for deterministic tests.
type fixedClock struct {
	t time.Time
}

func (c fixedClock) Now() time.Time { return c.t }

func TestNewJWTManagerRejectsShortSecret(t *testing.T) {
	_, err := NewJWTManager("short", time.Hour, realClock{})
	if err == nil {
		t.Fatal("expected error for secret < 32 bytes")
	}
	if !strings.Contains(err.Error(), "at least 32 bytes") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestNewJWTManagerRejectsNegativeTTL(t *testing.T) {
	secret := strings.Repeat("x", 32)
	_, err := NewJWTManager(secret, -1*time.Hour, realClock{})
	if err == nil {
		t.Fatal("expected error for negative TTL")
	}
	_, err = NewJWTManager(secret, 0, realClock{})
	if err == nil {
		t.Fatal("expected error for zero TTL")
	}
}

func TestNewJWTManagerSuccess(t *testing.T) {
	secret := strings.Repeat("s", 32)
	clock := fixedClock{t: time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)}
	mgr, err := NewJWTManager(secret, time.Hour, clock)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
}

func TestIssueAndParse(t *testing.T) {
	secret := strings.Repeat("k", 64)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{t: now}
	mgr, _ := NewJWTManager(secret, time.Hour, clock)

	user := &User{
		ID:      "user-uuid-1",
		Role:    RoleNurse,
		WardIDs: []string{"WARD-3A", "WARD-5B"},
	}

	tokenStr, expiresAt, err := mgr.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if tokenStr == "" {
		t.Fatal("expected non-empty token")
	}
	if !expiresAt.Equal(now.Add(time.Hour)) {
		t.Errorf("expiresAt = %v, want %v", expiresAt, now.Add(time.Hour))
	}

	claims, err := mgr.Parse(tokenStr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.Subject != "user-uuid-1" {
		t.Errorf("Subject = %q, want user-uuid-1", claims.Subject)
	}
	if claims.Role != "NURSE" {
		t.Errorf("Role = %q, want NURSE", claims.Role)
	}
	if len(claims.WardIDs) != 2 || claims.WardIDs[0] != "WARD-3A" || claims.WardIDs[1] != "WARD-5B" {
		t.Errorf("WardIDs = %v", claims.WardIDs)
	}
}

func TestParseExpiredToken(t *testing.T) {
	secret := strings.Repeat("k", 64)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{t: now}
	mgr, _ := NewJWTManager(secret, time.Hour, clock)

	user := &User{ID: "expired-user", Role: RoleNurse}
	tokenStr, _, err := mgr.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// Advance clock past expiry.
	mgr.clock = fixedClock{t: now.Add(2 * time.Hour)}

	_, err = mgr.Parse(tokenStr)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestParseExpiredJustAfterExpiry(t *testing.T) {
	secret := strings.Repeat("k", 64)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{t: now}
	mgr, _ := NewJWTManager(secret, time.Hour, clock)

	user := &User{ID: "edge-user", Role: RoleNurse}
	tokenStr, _, err := mgr.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// One nanosecond past expiry must fail.
	mgr.clock = fixedClock{t: now.Add(time.Hour + time.Nanosecond)}

	_, err = mgr.Parse(tokenStr)
	if err == nil {
		t.Fatal("expected error for token 1 ns past expiry")
	}
}

func TestParseActiveJustBeforeExpiry(t *testing.T) {
	secret := strings.Repeat("k", 64)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{t: now}
	mgr, _ := NewJWTManager(secret, time.Hour, clock)

	user := &User{ID: "valid-user", Role: RoleNurse}
	tokenStr, _, err := mgr.Issue(user)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	// One nanosecond before expiry must succeed.
	mgr.clock = fixedClock{t: now.Add(time.Hour - time.Nanosecond)}

	claims, err := mgr.Parse(tokenStr)
	if err != nil {
		t.Fatalf("Parse: token should be valid 1 ns before expiry: %v", err)
	}
	if claims.Subject != "valid-user" {
		t.Errorf("Subject = %q, want valid-user", claims.Subject)
	}
}

func TestParseMalformedToken(t *testing.T) {
	secret := strings.Repeat("k", 64)
	mgr, _ := NewJWTManager(secret, time.Hour, realClock{})

	_, err := mgr.Parse("not-a-jwt")
	if err == nil {
		t.Fatal("expected error for malformed token")
	}
}

func TestParseEmptyToken(t *testing.T) {
	secret := strings.Repeat("k", 64)
	mgr, _ := NewJWTManager(secret, time.Hour, realClock{})

	_, err := mgr.Parse("")
	if err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestParseWrongAlgorithm(t *testing.T) {
	secret := strings.Repeat("k", 64)
	mgr, _ := NewJWTManager(secret, time.Hour, realClock{})

	// Create a token signed with "none" algorithm.
	token := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{
		"sub": "fake-user",
	})
	noneToken, err := token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatalf("SignedString (none): %v", err)
	}

	_, err = mgr.Parse(noneToken)
	if err == nil {
		t.Fatal("expected error for 'none' algorithm token")
	}
}

func TestParseRSATokenRejected(t *testing.T) {
	secret := strings.Repeat("k", 64)
	mgr, _ := NewJWTManager(secret, time.Hour, realClock{})

	// Build a token that claims RS256 in the header but is actually
	// HMAC-signed — the Parse method should still reject it because
	// the algorithm in the header is not HMAC.
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": "x"})
	// Hack the header to pretend it's RS256.
	token.Header["alg"] = "RS256"
	signed, err := token.SignedString(mgr.secret)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}

	_, err = mgr.Parse(signed)
	if err == nil {
		t.Fatal("expected error for RS256-claiming token")
	}
}

func TestParseHS384TokenRejected(t *testing.T) {
	secret := "this-is-a-32-byte-minimum-test-secret"
	mgr, _ := NewJWTManager(secret, time.Hour, realClock{})
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, jwt.RegisteredClaims{
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	})
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	if _, err := mgr.Parse(signed); err == nil {
		t.Fatal("expected HS384 token to be rejected")
	}
}

func TestIssuedAtPresent(t *testing.T) {
	secret := strings.Repeat("k", 64)
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	clock := fixedClock{t: now}
	mgr, _ := NewJWTManager(secret, time.Hour, clock)

	user := &User{ID: "iat-user", Role: RoleAdmin}
	tokenStr, _, _ := mgr.Issue(user)

	claims, err := mgr.Parse(tokenStr)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if claims.IssuedAt == nil {
		t.Fatal("expected IssuedAt in claims")
	}
	if !claims.IssuedAt.Time.Equal(now) {
		t.Errorf("IssuedAt = %v, want %v", claims.IssuedAt.Time, now)
	}
}
