package mail

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestLogDriverMasksRecipientAndHidesBodyUnlessAsked(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	msg := Message{To: "jane@example.com", Subject: "Reset", Text: "open https://app.test/reset?token=TOPSECRET"}

	if err := NewLog(logger).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if out := buf.String(); strings.Contains(out, "jane@") || strings.Contains(out, "TOPSECRET") || !strings.Contains(out, "j***@example.com") {
		t.Fatalf("log driver output = %s", out)
	}

	buf.Reset()
	if err := NewLog(logger).WithBody(true).Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "TOPSECRET") {
		t.Fatalf("body must be logged when enabled: %s", buf.String())
	}
}
