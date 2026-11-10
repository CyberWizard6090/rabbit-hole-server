package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

func Setup(environment string, w io.Writer) *slog.Logger {
	if w == nil {
		w = os.Stdout
	}
	opts := &slog.HandlerOptions{Level: level(environment, os.Getenv("LOG_LEVEL"))}
	if environment != "development" {
		opts.ReplaceAttr = scrubAttr
	}

	var handler slog.Handler
	switch environment {
	case "production", "staging":
		handler = slog.NewJSONHandler(w, opts)
	default:
		handler = slog.NewTextHandler(w, opts)
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func level(environment, override string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(override)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	if environment == "development" {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}
