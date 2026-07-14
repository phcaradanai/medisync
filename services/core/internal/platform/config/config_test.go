package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Save and restore env for isolation.
	saveDB := os.Getenv("DATABASE_URL")
	os.Unsetenv("DATABASE_URL")
	os.Unsetenv("NATS_URL")
	os.Unsetenv("HTTP_ADDR")
	os.Unsetenv("LOG_LEVEL")
	os.Unsetenv("STARTUP_TIMEOUT_SECONDS")
	os.Unsetenv("JWT_EXPIRY_SECONDS")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "admin-bootstrap-for-tests")
	defer func() {
		os.Setenv("DATABASE_URL", saveDB)
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() with no env returned error: %v", err)
	}

	if cfg.DatabaseURL == "" {
		t.Error("DatabaseURL must have a default")
	}
	if cfg.NATSURL != "nats://localhost:4222" {
		t.Errorf("NATSURL default = %q, want nats://localhost:4222", cfg.NATSURL)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr default = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel default = %q, want info", cfg.LogLevel)
	}
	if cfg.StartupTimeoutSeconds != 60 {
		t.Errorf("StartupTimeoutSeconds default = %d, want 60", cfg.StartupTimeoutSeconds)
	}
	if cfg.JWTExpirySeconds != 3600 {
		t.Errorf("JWTExpirySeconds default = %d, want 3600", cfg.JWTExpirySeconds)
	}
	if cfg.AdminBootstrapPassword != "admin-bootstrap-for-tests" {
		t.Error("AdminBootstrapPassword should match the env override")
	}
}

func TestLoadOverrides(t *testing.T) {
	os.Setenv("NATS_URL", "nats://nats:4222")
	os.Setenv("HTTP_ADDR", ":9090")
	os.Setenv("LOG_LEVEL", "debug")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "override-password-here")
	defer func() {
		os.Unsetenv("NATS_URL")
		os.Unsetenv("HTTP_ADDR")
		os.Unsetenv("LOG_LEVEL")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.NATSURL != "nats://nats:4222" {
		t.Errorf("NATSURL = %q, want nats://nats:4222", cfg.NATSURL)
	}
	if cfg.HTTPAddr != ":9090" {
		t.Errorf("HTTPAddr = %q, want :9090", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
}

func TestLoadInvalidStartupTimeout(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"non-numeric", "abc"},
		{"zero", "0"},
		{"negative", "-5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
			os.Setenv("STARTUP_TIMEOUT_SECONDS", tt.value)
			defer func() {
				os.Unsetenv("STARTUP_TIMEOUT_SECONDS")
				os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
			}()

			_, err := Load()
			if err == nil {
				t.Errorf("Load() with STARTUP_TIMEOUT_SECONDS=%q should return an error", tt.value)
			}
		})
	}
}

func TestLoadValidStartupTimeout(t *testing.T) {
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	os.Setenv("STARTUP_TIMEOUT_SECONDS", "120")
	defer func() {
		os.Unsetenv("STARTUP_TIMEOUT_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.StartupTimeoutSeconds != 120 {
		t.Errorf("StartupTimeoutSeconds = %d, want 120", cfg.StartupTimeoutSeconds)
	}
}

func TestLoadJWTSecretDefault(t *testing.T) {
	os.Unsetenv("JWT_SECRET")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.JWTSecret == "" {
		t.Error("JWTSecret must have a default")
	}
}

func TestLoadJWTSecretOverride(t *testing.T) {
	os.Setenv("JWT_SECRET", "production-secret-key-thirtytwox")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.JWTSecret != "production-secret-key-thirtytwox" {
		t.Errorf("JWTSecret = %q, want production-secret-key-thirtytwox", cfg.JWTSecret)
	}
}

func TestLoadErrorMessageContainsValue(t *testing.T) {
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	os.Setenv("STARTUP_TIMEOUT_SECONDS", "bad")
	defer func() {
		os.Unsetenv("STARTUP_TIMEOUT_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad") {
		t.Errorf("error %q should mention the bad value", err.Error())
	}
}

// ── JWT expiry tests ──────────────────────────────────────────────

func TestLoadJWTExpiryDefault(t *testing.T) {
	os.Unsetenv("JWT_EXPIRY_SECONDS")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.JWTExpirySeconds != 3600 {
		t.Errorf("JWTExpirySeconds default = %d, want 3600", cfg.JWTExpirySeconds)
	}
}

func TestLoadJWTExpiryOverride(t *testing.T) {
	os.Setenv("JWT_EXPIRY_SECONDS", "7200")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("JWT_EXPIRY_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.JWTExpirySeconds != 7200 {
		t.Errorf("JWTExpirySeconds = %d, want 7200", cfg.JWTExpirySeconds)
	}
}

func TestLoadJWTExpiryZero(t *testing.T) {
	os.Setenv("JWT_EXPIRY_SECONDS", "0")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("JWT_EXPIRY_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for zero JWT expiry")
	}
}

func TestLoadJWTExpiryNegative(t *testing.T) {
	os.Setenv("JWT_EXPIRY_SECONDS", "-60")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("JWT_EXPIRY_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for negative JWT expiry")
	}
}

func TestLoadJWTExpiryNonNumeric(t *testing.T) {
	os.Setenv("JWT_EXPIRY_SECONDS", "abc")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("JWT_EXPIRY_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-numeric JWT expiry")
	}
}

// ── Admin bootstrap password tests ─────────────────────────────────

func TestLoadAdminPasswordEmpty(t *testing.T) {
	os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	defer os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing ADMIN_BOOTSTRAP_PASSWORD")
	}
	if !strings.Contains(err.Error(), "ADMIN_BOOTSTRAP_PASSWORD") {
		t.Errorf("error should mention ADMIN_BOOTSTRAP_PASSWORD: %v", err)
	}
}

func TestLoadAdminPasswordPlaceholder(t *testing.T) {
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "<admin-password-placeholder>")
	defer os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for placeholder admin password")
	}
	if !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("error should mention placeholder: %v", err)
	}
}

func TestLoadAdminPasswordTooShort(t *testing.T) {
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "abc")
	defer os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for too-short admin password")
	}
	if !strings.Contains(err.Error(), "at least") {
		t.Errorf("error should mention minimum length: %v", err)
	}
}

func TestLoadAdminPasswordChangeMe(t *testing.T) {
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "changeme")
	defer os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for 'changeme' admin password")
	}
}

func TestLoadAdminPasswordValid(t *testing.T) {
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "a-strong-bootstrap-password")
	defer os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.AdminBootstrapPassword != "a-strong-bootstrap-password" {
		t.Errorf("AdminBootstrapPassword = %q, want a-strong-bootstrap-password", cfg.AdminBootstrapPassword)
	}
}

// ── JWT secret validation tests ────────────────────────────────────

func TestLoadJWTSecretPlaceholder(t *testing.T) {
	os.Setenv("JWT_SECRET", "<jwt-signing-secret>")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for placeholder JWT secret")
	}
	if !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("error should mention placeholder: %v", err)
	}
}

func TestLoadJWTSecretTooShort(t *testing.T) {
	os.Setenv("JWT_SECRET", "short")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	os.Setenv("CARD_TOKEN_HMAC_KEY", "a-valid-card-token-hmac-key-for-test")
	defer func() {
		os.Unsetenv("JWT_SECRET")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
		os.Unsetenv("CARD_TOKEN_HMAC_KEY")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for too-short JWT secret")
	}
	if !strings.Contains(err.Error(), "at least") {
		t.Errorf("error should mention minimum length: %v", err)
	}
}

// ── Card-token HMAC key tests ──────────────────────────────────────

func TestLoadCardTokenHMACKeyDefault(t *testing.T) {
	os.Unsetenv("CARD_TOKEN_HMAC_KEY")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
		os.Unsetenv("CARD_TOKEN_HMAC_KEY")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.CardTokenHMACKey == "" {
		t.Error("CardTokenHMACKey must have a default")
	}
}

func TestLoadCardTokenHMACKeyOverride(t *testing.T) {
	os.Setenv("CARD_TOKEN_HMAC_KEY", "production-card-hmac-key-32-bytes!!")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("CARD_TOKEN_HMAC_KEY")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.CardTokenHMACKey != "production-card-hmac-key-32-bytes!!" {
		t.Errorf("CardTokenHMACKey = %q, want production-card-hmac-key-32-bytes!!", cfg.CardTokenHMACKey)
	}
}

func TestLoadCardTokenHMACKeyPlaceholder(t *testing.T) {
	os.Setenv("CARD_TOKEN_HMAC_KEY", "<card-token-hmac-key>")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("CARD_TOKEN_HMAC_KEY")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for placeholder card-token HMAC key")
	}
	if !strings.Contains(err.Error(), "placeholder") {
		t.Errorf("error should mention placeholder: %v", err)
	}
}

func TestLoadCardTokenHMACKeyTooShort(t *testing.T) {
	os.Setenv("CARD_TOKEN_HMAC_KEY", "short")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("CARD_TOKEN_HMAC_KEY")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for too-short card-token HMAC key")
	}
	if !strings.Contains(err.Error(), "at least") {
		t.Errorf("error should mention minimum length: %v", err)
	}
}

func TestLoadCardTokenHMACKeyChangeMe(t *testing.T) {
	os.Setenv("CARD_TOKEN_HMAC_KEY", "change-me")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("CARD_TOKEN_HMAC_KEY")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for 'change-me' card-token HMAC key")
	}
}

// ── Login rate limit tests ──────────────────────────────────────────

func TestLoadLoginRateLimitDefaults(t *testing.T) {
	os.Unsetenv("LOGIN_RATE_LIMIT_MAX")
	os.Unsetenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("LOGIN_RATE_LIMIT_MAX")
		os.Unsetenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LoginRateLimitMax != 10 {
		t.Errorf("LoginRateLimitMax default = %d, want 10", cfg.LoginRateLimitMax)
	}
	if cfg.LoginRateLimitWindowSeconds != 60 {
		t.Errorf("LoginRateLimitWindowSeconds default = %d, want 60", cfg.LoginRateLimitWindowSeconds)
	}
}

func TestLoadLoginRateLimitOverride(t *testing.T) {
	os.Setenv("LOGIN_RATE_LIMIT_MAX", "5")
	os.Setenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS", "30")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("LOGIN_RATE_LIMIT_MAX")
		os.Unsetenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LoginRateLimitMax != 5 {
		t.Errorf("LoginRateLimitMax = %d, want 5", cfg.LoginRateLimitMax)
	}
	if cfg.LoginRateLimitWindowSeconds != 30 {
		t.Errorf("LoginRateLimitWindowSeconds = %d, want 30", cfg.LoginRateLimitWindowSeconds)
	}
}

func TestLoadLoginRateLimitMaxDisable(t *testing.T) {
	os.Setenv("LOGIN_RATE_LIMIT_MAX", "0")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("LOGIN_RATE_LIMIT_MAX")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}
	if cfg.LoginRateLimitMax != 0 {
		t.Errorf("LoginRateLimitMax = %d, want 0 (disabled)", cfg.LoginRateLimitMax)
	}
}

func TestLoadLoginRateLimitMaxNegative(t *testing.T) {
	os.Setenv("LOGIN_RATE_LIMIT_MAX", "-1")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("LOGIN_RATE_LIMIT_MAX")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for negative LOGIN_RATE_LIMIT_MAX")
	}
}

func TestLoadLoginRateLimitMaxNonNumeric(t *testing.T) {
	os.Setenv("LOGIN_RATE_LIMIT_MAX", "abc")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("LOGIN_RATE_LIMIT_MAX")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-numeric LOGIN_RATE_LIMIT_MAX")
	}
}

func TestLoadLoginRateLimitWindowZero(t *testing.T) {
	os.Setenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS", "0")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for zero LOGIN_RATE_LIMIT_WINDOW_SECONDS")
	}
}

func TestLoadLoginRateLimitWindowNonNumeric(t *testing.T) {
	os.Setenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS", "xyz")
	os.Setenv("ADMIN_BOOTSTRAP_PASSWORD", "test-password-enough")
	defer func() {
		os.Unsetenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS")
		os.Unsetenv("ADMIN_BOOTSTRAP_PASSWORD")
	}()

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for non-numeric LOGIN_RATE_LIMIT_WINDOW_SECONDS")
	}
}
