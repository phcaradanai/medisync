package logging

import (
	"log/slog"
	"testing"
)

func TestNewDebugLevel(t *testing.T) {
	l := New("debug")
	if !l.Handler().Enabled(nil, slog.LevelDebug) {
		t.Error("debug level should enable Debug logs")
	}
}

func TestNewInfoLevel(t *testing.T) {
	l := New("info")
	if !l.Handler().Enabled(nil, slog.LevelInfo) {
		t.Error("info level should enable Info logs")
	}
	if l.Handler().Enabled(nil, slog.LevelDebug) {
		t.Error("info level should not enable Debug logs")
	}
}

func TestNewWarnLevel(t *testing.T) {
	l := New("warn")
	if !l.Handler().Enabled(nil, slog.LevelWarn) {
		t.Error("warn level should enable Warn logs")
	}
	if l.Handler().Enabled(nil, slog.LevelInfo) {
		t.Error("warn level should not enable Info logs")
	}
}

func TestNewErrorLevel(t *testing.T) {
	l := New("error")
	if !l.Handler().Enabled(nil, slog.LevelError) {
		t.Error("error level should enable Error logs")
	}
	if l.Handler().Enabled(nil, slog.LevelWarn) {
		t.Error("error level should not enable Warn logs")
	}
}

func TestNewUnknownLevelDefaultsToInfo(t *testing.T) {
	l := New("verbose")
	if l.Handler().Enabled(nil, slog.LevelDebug) {
		t.Error("unknown level should default to Info, not enable Debug")
	}
	if !l.Handler().Enabled(nil, slog.LevelInfo) {
		t.Error("unknown level should default to Info, enable Info")
	}
}

func TestNewEmptyStringDefaultsToInfo(t *testing.T) {
	l := New("")
	if !l.Handler().Enabled(nil, slog.LevelInfo) {
		t.Error("empty level should default to Info")
	}
	if l.Handler().Enabled(nil, slog.LevelDebug) {
		t.Error("empty level should not enable Debug")
	}
}
