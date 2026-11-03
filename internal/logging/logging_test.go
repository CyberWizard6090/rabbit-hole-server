package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestSetupUsesJSONInProductionAndTextInDevelopment(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")

	var prod bytes.Buffer
	Setup("production", &prod).Info("hello", "k", "v")
	var entry map[string]any
	if err := json.Unmarshal(prod.Bytes(), &entry); err != nil || entry["msg"] != "hello" || entry["k"] != "v" {
		t.Fatalf("production output is not JSON with the expected fields: %q (%v)", prod.String(), err)
	}

	var dev bytes.Buffer
	Setup("development", &dev).Info("hello", "k", "v")
	if json.Valid(dev.Bytes()) || !strings.Contains(dev.String(), "msg=hello") {
		t.Fatalf("development output should be logfmt text: %q", dev.String())
	}
}

func TestLevelDefaultsAndOverride(t *testing.T) {
	cases := []struct {
		env, override string
		want          slog.Level
	}{
		{"development", "", slog.LevelDebug},
		{"production", "", slog.LevelInfo},
		{"test", "", slog.LevelInfo},
		{"production", "debug", slog.LevelDebug},
		{"development", "ERROR", slog.LevelError},
		{"production", "nonsense", slog.LevelInfo},
		{"production", "warning", slog.LevelWarn},
	}
	for _, c := range cases {
		if got := level(c.env, c.override); got != c.want {
			t.Errorf("level(%q,%q) = %v, want %v", c.env, c.override, got, c.want)
		}
	}
}

func TestSetupInstallsDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv("LOG_LEVEL", "")
	Setup("production", &buf)
	slog.Info("via default")
	if !strings.Contains(buf.String(), "via default") {
		t.Fatalf("slog default was not replaced: %q", buf.String())
	}
}
