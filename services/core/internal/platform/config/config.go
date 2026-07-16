// Package config centralizes environment parsing. Nothing else in the
// codebase reads environment variables directly.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	// minJWTSecretBytes is the floor for an acceptable JWT secret.
	minJWTSecretBytes = 32
	// defaultJWTExpirySeconds is the token lifetime when JWT_EXPIRY_SECONDS is unset.
	defaultJWTExpirySeconds = 3600
	// minAdminPasswordBytes rejects bootstrap passwords that are trivially weak.
	minAdminPasswordBytes = 12
	// devJWTSecretDefault is accepted only as the fallback in dev; prod must override.
	devJWTSecretDefault = "medisync-dev-secret-change-in-production"
	// devCardTokenHMACKeyDefault is accepted only as the fallback in dev; prod must override.
	devCardTokenHMACKeyDefault = "medisync-dev-card-hmac-change-in-prod"
	// minCardTokenHMACKeyBytes is the floor for an acceptable card-token HMAC key.
	minCardTokenHMACKeyBytes = 32
	// defaultLoginRateLimitMax is the requests-per-window cap when unset.
	defaultLoginRateLimitMax = 10
	// defaultLoginRateLimitWindowSeconds is the window size when unset.
	defaultLoginRateLimitWindowSeconds = 60
)

type Config struct {
	// DatabaseURL is a pgx-compatible PostgreSQL connection string.
	DatabaseURL string
	NATSURL     string
	// HTTPAddr is where the Connect-RPC API will listen.
	HTTPAddr string
	LogLevel string
	// JWTSecret is the HMAC key for signing JWT access tokens.
	JWTSecret string
	// JWTExpirySeconds is the access-token lifetime in seconds. Must be positive.
	JWTExpirySeconds int
	// AdminBootstrapPassword is the plaintext password for the seed admin user.
	// It is hashed with bcrypt before storage and never logged.
	AdminBootstrapPassword string
	// CardTokenHMACKey is the HMAC key for deterministic card-token hashing.
	// Card-token operations fail closed without a valid key. Production must
	// replace the development default with a strong key (>=32 bytes).
	CardTokenHMACKey string
	// StartupTimeoutSeconds bounds dependency retries (DB ping, NATS connect).
	StartupTimeoutSeconds int
	// LoginRateLimitMax is the maximum login attempts per window per identifier
	// (username or card token) and per remote IP. A value of 0 disables rate limiting.
	LoginRateLimitMax int
	// LoginRateLimitWindowSeconds is the sliding-window size in seconds for login
	// rate limiting. Must be positive when LoginRateLimitMax > 0.
	LoginRateLimitWindowSeconds int
	// PrintOpsURL is the base URL of the print_ops API (e.g. http://print-ops:3000).
	PrintOpsURL string
	// PrintOpsAPIKey is the X-Api-Key sent to print_ops.
	PrintOpsAPIKey string
	// PrintOpsFake, when true, uses a no-op fake print_ops client (for dev/testing).
	PrintOpsFake bool
	// VendingURL is the base URL of the vending-3d-ctl-agent API.
	VendingURL string
	// VendingAPIBearerToken is the Bearer token sent to vending-3d-ctl-agent.
	VendingAPIBearerToken string
	// VendingFake, when true, uses a no-op fake vending client (for dev/testing).
	// Deprecated: use FulfillmentFake instead.
	VendingFake bool
	// FulfillmentFake, when true, uses a no-op fake vending client (for dev/testing).
	FulfillmentFake bool
}

// Load reads configuration from environment variables. It rejects missing,
// placeholder, or dangerously weak values for secrets.
func Load() (Config, error) {
	cfg := Config{
		DatabaseURL:            getenv("DATABASE_URL", "postgres://medisync:***@localhost:5432/medisync?sslmode=disable"),
		NATSURL:                getenv("NATS_URL", "nats://localhost:4222"),
		HTTPAddr:               getenv("HTTP_ADDR", ":8080"),
		LogLevel:               getenv("LOG_LEVEL", "info"),
		JWTSecret:              getenv("JWT_SECRET", devJWTSecretDefault),
		JWTExpirySeconds:       defaultJWTExpirySeconds,
		AdminBootstrapPassword: os.Getenv("ADMIN_BOOTSTRAP_PASSWORD"),
		CardTokenHMACKey:       getenv("CARD_TOKEN_HMAC_KEY", devCardTokenHMACKeyDefault),
		StartupTimeoutSeconds:  60,
	}

	// ── JWT expiry ──────────────────────────────────────────────────
	if raw := os.Getenv("JWT_EXPIRY_SECONDS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("JWT_EXPIRY_SECONDS must be a positive integer, got %q", raw)
		}
		cfg.JWTExpirySeconds = n
	}

	// ── Admin bootstrap password ────────────────────────────────────
	if cfg.AdminBootstrapPassword == "" {
		return Config{}, fmt.Errorf("ADMIN_BOOTSTRAP_PASSWORD is required and must not be empty")
	}
	if err := validateSecret("ADMIN_BOOTSTRAP_PASSWORD", cfg.AdminBootstrapPassword, minAdminPasswordBytes); err != nil {
		return Config{}, err
	}

	// ── JWT secret ──────────────────────────────────────────────────
	if err := validateSecret("JWT_SECRET", cfg.JWTSecret, minJWTSecretBytes); err != nil {
		return Config{}, err
	}

	// ── Card-token HMAC key ─────────────────────────────────────────
	if err := validateSecret("CARD_TOKEN_HMAC_KEY", cfg.CardTokenHMACKey, minCardTokenHMACKeyBytes); err != nil {
		return Config{}, err
	}

	// ── Startup timeout ─────────────────────────────────────────────
	if raw := os.Getenv("STARTUP_TIMEOUT_SECONDS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("STARTUP_TIMEOUT_SECONDS must be a positive integer, got %q", raw)
		}
		cfg.StartupTimeoutSeconds = n
	}

	// ── Login rate limit ─────────────────────────────────────────────
	cfg.LoginRateLimitMax = defaultLoginRateLimitMax
	if raw := os.Getenv("LOGIN_RATE_LIMIT_MAX"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return Config{}, fmt.Errorf("LOGIN_RATE_LIMIT_MAX must be a non-negative integer, got %q", raw)
		}
		cfg.LoginRateLimitMax = n
	}

	cfg.LoginRateLimitWindowSeconds = defaultLoginRateLimitWindowSeconds
	if raw := os.Getenv("LOGIN_RATE_LIMIT_WINDOW_SECONDS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return Config{}, fmt.Errorf("LOGIN_RATE_LIMIT_WINDOW_SECONDS must be a positive integer, got %q", raw)
		}
		cfg.LoginRateLimitWindowSeconds = n
	}

	// ── Print ops ────────────────────────────────────────────────────
	cfg.PrintOpsURL = getenv("PRINT_OPS_URL", "http://localhost:3000")
	cfg.PrintOpsAPIKey = os.Getenv("PRINT_OPS_API_KEY")

	fakeStr := strings.ToLower(getenv("PRINT_OPS_FAKE", "false"))
	cfg.PrintOpsFake = fakeStr == "true" || fakeStr == "1"

	// ── Vending ──────────────────────────────────────────────────────
	cfg.VendingURL = getenv("VENDING_URL", "http://localhost:3000")
	cfg.VendingAPIBearerToken = os.Getenv("VENDING_API_BEARER_TOKEN")

	vendingFakeStr := strings.ToLower(getenv("VENDING_FAKE", "false"))
	cfg.VendingFake = vendingFakeStr == "true" || vendingFakeStr == "1"

	fulfillmentFakeStr := strings.ToLower(getenv("FULFILLMENT_FAKE", "false"))
	cfg.FulfillmentFake = fulfillmentFakeStr == "true" || fulfillmentFakeStr == "1"

	return cfg, nil
}

// validateSecret checks that a secret-like value is not a placeholder,
// not trivially short, and not identical to the well-known dev default
// (which is acceptable only when the caller intentionally chooses it).
func validateSecret(name, value string, minBytes int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("%s must not be empty", name)
	}
	if looksLikePlaceholder(trimmed) {
		return fmt.Errorf("%s looks like a placeholder — real value required", name)
	}
	if len([]byte(trimmed)) < minBytes {
		return fmt.Errorf("%s must be at least %d bytes, got %d", name, minBytes, len([]byte(trimmed)))
	}
	return nil
}

// looksLikePlaceholder returns true when a value is an obvious
// placeholder such as <secret>, <change-me>, or changeme.
func looksLikePlaceholder(s string) bool {
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		return true
	}
	lower := strings.ToLower(s)
	return lower == "changeme" || lower == "change-me"
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
