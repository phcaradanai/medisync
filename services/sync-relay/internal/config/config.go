package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	PrintOpsURL            string
	PrintOpsAPIKey         string
	NATSURL                string
	PollInterval           time.Duration
	PollPageSize           int
	StartupLookbackMinutes int
	CursorDBPath           string
	LogLevel               string
}

func Load() Config {
	return Config{
		PrintOpsURL:            env("PRINTOPS_URL", "http://printops-api:3001"),
		PrintOpsAPIKey:         env("PRINTOPS_API_KEY", "printops-dev-apikey-2026"),
		NATSURL:                env("NATS_URL", "nats://nats:4222"),
		PollInterval:           time.Duration(envInt("POLL_INTERVAL_SECONDS", 5)) * time.Second,
		PollPageSize:           envInt("POLL_PAGE_SIZE", 50),
		StartupLookbackMinutes: envInt("STARTUP_LOOKBACK_MINUTES", 5),
		CursorDBPath:           env("CURSOR_DB_PATH", "/data/sync-relay.db"),
		LogLevel:               env("LOG_LEVEL", "info"),
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
