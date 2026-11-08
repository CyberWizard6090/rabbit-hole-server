package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestApplySessionTimeouts(t *testing.T) {
	cfg, err := pgx.ParseConfig("host=127.0.0.1 user=u dbname=d lock_timeout=777")
	if err != nil {
		t.Fatal(err)
	}
	applySessionTimeouts(cfg, Options{StatementTimeout: 30 * time.Second, LockTimeout: 5 * time.Second, IdleInTxnTimeout: time.Minute})
	want := map[string]string{
		"statement_timeout":                   "30000",
		"lock_timeout":                        "777",
		"idle_in_transaction_session_timeout": "60000",
	}
	for name, value := range want {
		if cfg.RuntimeParams[name] != value {
			t.Errorf("%s = %q, want %q", name, cfg.RuntimeParams[name], value)
		}
	}

	cfg, _ = pgx.ParseConfig("host=127.0.0.1 user=u dbname=d")
	applySessionTimeouts(cfg, Options{})
	for _, name := range []string{"statement_timeout", "lock_timeout", "idle_in_transaction_session_timeout"} {
		if _, set := cfg.RuntimeParams[name]; set {
			t.Errorf("%s must stay unset when the timeout is 0", name)
		}
	}
}

func TestGormLoggerLevelsFollowTheEnvironment(t *testing.T) {
	cases := map[string][]string{
		"production":  {"error"},
		"staging":     {"warn", "error"},
		"test":        {"warn", "error"},
		"development": {"info", "warn", "error"},
	}
	for environment, want := range cases {
		var buf bytes.Buffer
		logger := gormLogger(Options{Environment: environment, Logger: slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))})
		logger.Info(context.Background(), "info-line")
		logger.Warn(context.Background(), "warn-line")
		logger.Error(context.Background(), "error-line")
		for _, level := range []string{"info", "warn", "error"} {
			got := strings.Contains(buf.String(), level+"-line")
			if expected := slices.Contains(want, level); got != expected {
				t.Errorf("%s: %s logged = %v, want %v", environment, level, got, expected)
			}
		}
	}
}

func TestGormLoggerIsJSONSlogAndReportsSlowQueries(t *testing.T) {
	var buf bytes.Buffer
	logger := gormLogger(Options{
		Environment: "production", SlowQueryThreshold: time.Millisecond,
		Logger: slog.New(slog.NewJSONHandler(&buf, nil)),
	})
	logger.Trace(context.Background(), time.Now().Add(-time.Second), func() (string, int64) { return "SELECT 1", 1 }, nil)
	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("gorm output is not a JSON log line: %v: %q", err, buf.String())
	}
	if entry["level"] != "WARN" {
		t.Fatalf("slow query entry = %v", entry)
	}
}

func TestExpectedSchemaVersionMatchesTheLastMigration(t *testing.T) {
	version, err := ExpectedSchemaVersion()
	if err != nil || version < 2 {
		t.Fatalf("expected version = %d, err = %v", version, err)
	}
}

func TestSchemaErrorMessagesTellWhatToDo(t *testing.T) {
	cases := []struct {
		err  *SchemaError
		want []string
	}{
		{&SchemaError{Expected: 2, Missing: true}, []string{"no migration history", "migrate -action up"}},
		{&SchemaError{Current: 1, Expected: 2}, []string{"version 1", "expects version 2", "migrate -action up"}},
		{&SchemaError{Current: 2, Expected: 2, Dirty: true}, []string{"dirty", "force"}},
	}
	for _, tc := range cases {
		var err error = tc.err
		var target *SchemaError
		if !errors.As(err, &target) {
			t.Fatal("not a SchemaError")
		}
		for _, fragment := range tc.want {
			if !strings.Contains(err.Error(), fragment) {
				t.Errorf("%q does not mention %q", err.Error(), fragment)
			}
		}
	}
}
