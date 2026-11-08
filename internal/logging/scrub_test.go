package logging

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestScrubMasksEmailsAndSecrets(t *testing.T) {
	cases := []struct{ in, mustNotContain string }{
		{"cannot send to jane.doe@example.com now", "jane.doe"},
		{"GET /verify?token=abc123SECRET&x=1", "abc123SECRET"},
		{`password="hunter2" failed`, "hunter2"},
		{"Authorization: Bearer eyJhbGciOi.payload.sig", "eyJhbGciOi"},
		{"api_key: sk-live-999", "sk-live-999"},
		{"hash=$argon2id$v=19$m=65536,t=3,p=4$salt$key", "$argon2id"},
	}
	for _, c := range cases {
		if got := Scrub(c.in); strings.Contains(got, c.mustNotContain) {
			t.Errorf("Scrub(%q) = %q still contains %q", c.in, got, c.mustNotContain)
		}
	}
	if got := Scrub("plain message"); got != "plain message" {
		t.Fatalf("plain text changed: %q", got)
	}
	if got := MaskEmail("jane@example.com"); got != "j***@example.com" {
		t.Fatalf("MaskEmail = %q", got)
	}
}

func TestSetupScrubsOutsideDevelopment(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	var prod bytes.Buffer
	Setup("production", &prod).Error("delivery failed", "error", errors.New("RCPT TO: 550 <victim@example.org> rejected"), "url", "https://x.test/?token=SECRETVALUE")
	if strings.Contains(prod.String(), "victim@") || strings.Contains(prod.String(), "SECRETVALUE") {
		t.Fatalf("production log leaks data: %s", prod.String())
	}
	var entry map[string]any
	if err := json.Unmarshal(prod.Bytes(), &entry); err != nil {
		t.Fatalf("not JSON: %v", err)
	}

	var dev bytes.Buffer
	Setup("development", &dev).Info("mail", "text", "link ?token=DEVTOKEN")
	if !strings.Contains(dev.String(), "DEVTOKEN") {
		t.Fatalf("development keeps the raw text so mail links stay usable: %s", dev.String())
	}
}
