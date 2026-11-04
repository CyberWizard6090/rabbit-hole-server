package config

import (
	"strings"
	"testing"
	"time"
)

func TestMailDefaultsAreForLocalDevelopment(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := cfg.Mail
	if m.Driver != "log" || m.SMTPPort != 587 || m.SMTPSecurity != "starttls" || m.RequireVerifiedEmail {
		t.Fatalf("mail = %+v", m)
	}
	if m.VerificationTTL != 48*time.Hour || m.ResetTTL != time.Hour {
		t.Fatalf("ttls = %v, %v", m.VerificationTTL, m.ResetTTL)
	}
	if m.FrontendURL != "http://localhost:3000" {
		t.Fatalf("frontend = %q", m.FrontendURL)
	}
}

func TestMailRejectsBadValues(t *testing.T) {
	cases := map[string]map[string]string{
		"smtp without host":  {"MAIL_DRIVER": "smtp"},
		"unknown driver":     {"MAIL_DRIVER": "carrier-pigeon"},
		"bad from":           {"MAIL_FROM": "not an address"},
		"from with newline":  {"MAIL_FROM": "a@example.com\r\nBcc: b@example.com"},
		"bad port":           {"SMTP_PORT": "70000"},
		"unknown security":   {"SMTP_SECURITY": "maybe"},
		"zero verification":  {"EMAIL_VERIFICATION_TTL": "0s"},
		"negative reset ttl": {"PASSWORD_RESET_TTL": "-1h"},
		"bad frontend url":   {"FRONTEND_URL": "not a url"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			baseEnv(t)
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatal("expected the configuration to be rejected")
			}
		})
	}
}

func TestMailAcceptsACompleteSMTPSetup(t *testing.T) {
	baseEnv(t)
	t.Setenv("MAIL_DRIVER", "smtp")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("SMTP_USERNAME", "apikey")
	t.Setenv("SMTP_PASSWORD", "secret")
	t.Setenv("MAIL_FROM", "Rabbit Hole <no-reply@example.com>")
	t.Setenv("REQUIRE_VERIFIED_EMAIL", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Mail.SMTPHost != "smtp.example.com" || !cfg.Mail.RequireVerifiedEmail {
		t.Fatalf("mail = %+v", cfg.Mail)
	}
}

func TestDeployedEnvironmentsNeedRealMail(t *testing.T) {
	const db = "postgres://app@db/rabbit_hole?sslmode=require"
	setup := func(t *testing.T, env map[string]string) error {
		t.Helper()
		t.Setenv("APP_ENV", "production")
		t.Setenv("JWT_SECRET", strongSecret)
		t.Setenv("ALLOWED_ORIGINS", "https://app.example.com")
		t.Setenv("DB_URL", db)
		t.Setenv("MAIL_DRIVER", "smtp")
		t.Setenv("SMTP_HOST", "smtp.example.com")
		t.Setenv("FRONTEND_URL", "https://app.example.com")
		for k, v := range env {
			t.Setenv(k, v)
		}
		_, err := Load()
		return err
	}
	if err := setup(t, nil); err != nil {
		t.Fatalf("a complete production setup was rejected: %v", err)
	}
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"log driver":                 {map[string]string{"MAIL_DRIVER": "log"}, "MAIL_DRIVER=log"},
		"http frontend":              {map[string]string{"FRONTEND_URL": "http://app.example.com"}, "FRONTEND_URL"},
		"cleartext to a remote host": {map[string]string{"SMTP_SECURITY": "none"}, "SMTP_SECURITY=none"},
	} {
		t.Run(name, func(t *testing.T) {
			err := setup(t, tc.env)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
	if err := setup(t, map[string]string{"SMTP_SECURITY": "none", "SMTP_HOST": "127.0.0.1"}); err != nil {
		t.Fatalf("a local relay without TLS should be allowed: %v", err)
	}
}
