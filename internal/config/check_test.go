package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const (
	testDBSecretMarker = "hunter2-db-password"
	testSMTPPassword   = "smtp-pass-ZZ9"
	testS3Secret       = "s3-secret-QQ7"
)

func cleanEnv(t *testing.T) {
	t.Helper()
	for _, target := range []any{&Config{}, &DBConfig{}} {
		for _, name := range envNames(target, "") {
			t.Setenv(name, "")
			_ = os.Unsetenv(name)
		}
	}
}

func validEnv(t *testing.T, overrides map[string]string) error {
	t.Helper()
	cleanEnv(t)
	base := map[string]string{
		"DB_URL":     "postgres://app:" + testDBSecretMarker + "@db/rabbit_hole",
		"JWT_SECRET": strongSecret,
	}
	for k, v := range overrides {
		base[k] = v
	}
	for k, v := range base {
		if v == "-" {
			continue
		}
		t.Setenv(k, v)
	}
	_, err := Load()
	return err
}

func problemsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	var verr *ValidationError
	if !errors.As(err, &verr) {
		t.Fatalf("error %v is not a *ValidationError", err)
	}
	out := map[string]string{}
	for _, p := range verr.Problems {
		out[p.Var] = p.Msg
	}
	return out
}

func s3Env(skip string) map[string]string {
	env := map[string]string{
		"STORAGE_DRIVER": "s3", "S3_ENDPOINT": "minio:9000", "S3_BUCKET": "b",
		"S3_ACCESS_KEY": "a", "S3_SECRET_KEY": "s",
	}
	if skip != "" {
		env[skip] = "-"
	}
	return env
}

func TestValidConfigurationLoads(t *testing.T) {
	if err := validEnv(t, nil); err != nil {
		t.Fatalf("Load: %v", err)
	}
}

func TestEveryRuleNamesItsVariable(t *testing.T) {
	endpointWithScheme := s3Env("")
	endpointWithScheme["S3_ENDPOINT"] = "https://minio:9000"
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"missing DB_URL", map[string]string{"DB_URL": "-"}, "DB_URL"},
		{"missing JWT_SECRET", map[string]string{"JWT_SECRET": "-"}, "JWT_SECRET"},
		{"short JWT_SECRET", map[string]string{"JWT_SECRET": "too-short"}, "JWT_SECRET"},
		{"unparsable access ttl", map[string]string{"JWT_ACCESS_TTL": "abc"}, "JWT_ACCESS_TTL"},
		{"unparsable refresh ttl", map[string]string{"JWT_REFRESH_TTL": "1 week"}, "JWT_REFRESH_TTL"},
		{"zero access ttl", map[string]string{"JWT_ACCESS_TTL": "0s"}, "JWT_ACCESS_TTL"},
		{"unparsable port", map[string]string{"PORT": "eighty"}, "PORT"},
		{"port out of range", map[string]string{"PORT": "70000"}, "PORT"},
		{"unparsable pool size", map[string]string{"DB_MAX_OPEN_CONNS": "many"}, "DB_MAX_OPEN_CONNS"},
		{"unparsable bool", map[string]string{"REQUIRE_VERIFIED_EMAIL": "sometimes"}, "REQUIRE_VERIFIED_EMAIL"},
		{"unparsable bytes", map[string]string{"MAX_IMAGE_BYTES": "10MB"}, "MAX_IMAGE_BYTES"},
		{"unparsable throttle", map[string]string{"NOTIFICATION_EMAIL_PER_HOUR": "x"}, "NOTIFICATION_EMAIL_PER_HOUR"},
		{"unparsable verification ttl", map[string]string{"EMAIL_VERIFICATION_TTL": "2days"}, "EMAIL_VERIFICATION_TTL"},
		{"unknown environment", map[string]string{"APP_ENV": "prod"}, "APP_ENV"},
		{"unknown mail driver", map[string]string{"MAIL_DRIVER": "pigeon"}, "MAIL_DRIVER"},
		{"smtp without host", map[string]string{"MAIL_DRIVER": "smtp"}, "SMTP_HOST"},
		{"smtp bad port", map[string]string{"SMTP_PORT": "0"}, "SMTP_PORT"},
		{"smtp bad security", map[string]string{"SMTP_SECURITY": "maybe"}, "SMTP_SECURITY"},
		{"smtp username without password", map[string]string{"MAIL_DRIVER": "smtp", "SMTP_HOST": "smtp.example.com", "SMTP_USERNAME": "u"}, "SMTP_PASSWORD"},
		{"unparsable from", map[string]string{"MAIL_FROM": "not an address"}, "MAIL_FROM"},
		{"from with header injection", map[string]string{"MAIL_FROM": "a@example.com\r\nBcc: b@example.com"}, "MAIL_FROM"},
		{"bad frontend url", map[string]string{"FRONTEND_URL": "app.example.com"}, "FRONTEND_URL"},
		{"frontend url with ftp scheme", map[string]string{"FRONTEND_URL": "ftp://app.example.com"}, "FRONTEND_URL"},
		{"bad public base url", map[string]string{"PUBLIC_BASE_URL": "api.example.com"}, "PUBLIC_BASE_URL"},
		{"unknown storage driver", map[string]string{"STORAGE_DRIVER": "ftp"}, "STORAGE_DRIVER"},
		{"s3 endpoint with scheme", endpointWithScheme, "S3_ENDPOINT"},
		{"s3 missing bucket", s3Env("S3_BUCKET"), "S3_BUCKET"},
		{"s3 missing access key", s3Env("S3_ACCESS_KEY"), "S3_ACCESS_KEY"},
		{"s3 missing secret key", s3Env("S3_SECRET_KEY"), "S3_SECRET_KEY"},
		{"s3 missing endpoint", s3Env("S3_ENDPOINT"), "S3_ENDPOINT"},
		{"empty origins", map[string]string{"ALLOWED_ORIGINS": ","}, "ALLOWED_ORIGINS"},
		{"origin without scheme", map[string]string{"ALLOWED_ORIGINS": "app.example.com"}, "ALLOWED_ORIGINS"},
		{"origin with trailing slash", map[string]string{"ALLOWED_ORIGINS": "http://localhost:3000/"}, "ALLOWED_ORIGINS"},
		{"origin with path", map[string]string{"ALLOWED_ORIGINS": "http://localhost:3000/app"}, "ALLOWED_ORIGINS"},
		{"unparsable proxies", map[string]string{"TRUSTED_PROXIES": "127.0.0.1,proxy.local"}, "TRUSTED_PROXIES"},
		{"bad cidr", map[string]string{"TRUSTED_PROXIES": "10.0.0.0/99"}, "TRUSTED_PROXIES"},
		{"bad host", map[string]string{"HOST": "not a host"}, "HOST"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validEnv(t, tc.env)
			if err == nil {
				t.Fatal("expected the configuration to be rejected")
			}
			problems := problemsOf(t, err)
			msg, ok := problems[tc.want]
			if !ok {
				t.Fatalf("no problem for %s in %v", tc.want, err)
			}
			if strings.TrimSpace(msg) == "" || !strings.Contains(err.Error(), tc.want+":") {
				t.Fatalf("unhelpful message %q", msg)
			}
		})
	}
}

func TestAcceptedValues(t *testing.T) {
	cases := map[string]map[string]string{
		"proxy list with cidr":  {"TRUSTED_PROXIES": "127.0.0.1,::1,10.0.0.0/8,fd00::/8"},
		"two origins":           {"ALLOWED_ORIGINS": "http://localhost:3000,https://app.example.com"},
		"s3 complete":           s3Env(""),
		"smtp complete":         {"MAIL_DRIVER": "smtp", "SMTP_HOST": "smtp.example.com", "SMTP_USERNAME": "u", "SMTP_PASSWORD": "p", "SMTP_SECURITY": "tls", "SMTP_PORT": "465"},
		"smtp without login":    {"MAIL_DRIVER": "smtp", "SMTP_HOST": "127.0.0.1", "SMTP_PORT": "1025", "SMTP_SECURITY": "none"},
		"placeholder in dev":    {"JWT_SECRET": "replace-with-at-least-32-characters"},
		"placeholder in test":   {"APP_ENV": "test", "JWT_SECRET": "replace-with-at-least-32-characters"},
		"named from":            {"MAIL_FROM": "Rabbit Hole <no-reply@example.com>"},
		"durations and numbers": {"JWT_ACCESS_TTL": "15m", "PORT": "9000", "DB_MAX_OPEN_CONNS": "10"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validEnv(t, env); err != nil {
				t.Fatalf("rejected: %v", err)
			}
		})
	}
}

func TestDeployedRulesAreStricter(t *testing.T) {
	deployed := map[string]string{
		"APP_ENV": "production", "MAIL_DRIVER": "smtp", "SMTP_HOST": "smtp.example.com",
		"FRONTEND_URL": "https://app.example.com", "ALLOWED_ORIGINS": "https://app.example.com",
		"PUBLIC_BASE_URL": "https://api.example.com", "METRICS_TOKEN": "metrics-token-for-tests-0123",
	}
	merge := func(extra map[string]string) map[string]string {
		out := map[string]string{}
		for k, v := range deployed {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	if err := validEnv(t, merge(nil)); err != nil {
		t.Fatalf("safe production configuration rejected: %v", err)
	}
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"placeholder secret", map[string]string{"JWT_SECRET": "replace-with-at-least-32-characters"}, "JWT_SECRET"},
		{"low entropy secret", map[string]string{"JWT_SECRET": "abababababababababababababababab"}, "JWT_SECRET"},
		{"log driver", map[string]string{"MAIL_DRIVER": "log"}, "MAIL_DRIVER"},
		{"http frontend", map[string]string{"FRONTEND_URL": "http://app.example.com"}, "FRONTEND_URL"},
		{"http origin", map[string]string{"ALLOWED_ORIGINS": "http://app.example.com"}, "ALLOWED_ORIGINS"},
		{"http public base url", map[string]string{"PUBLIC_BASE_URL": "http://api.example.com"}, "PUBLIC_BASE_URL"},
		{"cleartext smtp", map[string]string{"SMTP_SECURITY": "none"}, "SMTP_SECURITY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validEnv(t, merge(tc.env))
			if err == nil {
				t.Fatal("expected rejection")
			}
			if _, ok := problemsOf(t, err)[tc.want]; !ok {
				t.Fatalf("no problem for %s in %v", tc.want, err)
			}
		})
	}
}

func TestAllProblemsAreReportedAtOnce(t *testing.T) {
	err := validEnv(t, map[string]string{
		"DB_URL": "-", "JWT_SECRET": "short", "JWT_ACCESS_TTL": "abc", "PORT": "x",
		"MAIL_DRIVER": "smtp", "ALLOWED_ORIGINS": "nope", "TRUSTED_PROXIES": "proxy.local",
		"STORAGE_DRIVER": "s3", "FRONTEND_URL": "nope",
	})
	if err == nil {
		t.Fatal("expected rejection")
	}
	problems := problemsOf(t, err)
	for _, name := range []string{
		"DB_URL", "JWT_SECRET", "JWT_ACCESS_TTL", "PORT", "SMTP_HOST", "ALLOWED_ORIGINS", "TRUSTED_PROXIES",
		"S3_ENDPOINT", "S3_BUCKET", "S3_ACCESS_KEY", "S3_SECRET_KEY", "FRONTEND_URL",
	} {
		if _, ok := problems[name]; !ok {
			t.Errorf("problem for %s missing in:\n%v", name, err)
		}
	}
	if !strings.Contains(err.Error(), "problems found") {
		t.Errorf("summary line missing: %v", err)
	}
}

func TestTypeFailureDoesNotCauseFollowUpProblems(t *testing.T) {
	err := validEnv(t, map[string]string{"JWT_ACCESS_TTL": "abc"})
	if got := len(problemsOf(t, err)); got != 1 {
		t.Fatalf("problems = %d, want 1: %v", got, err)
	}
}

func TestErrorTextNeverContainsSecrets(t *testing.T) {
	err := validEnv(t, map[string]string{
		"JWT_SECRET": "short-secret-value", "SMTP_PASSWORD": testSMTPPassword, "MAIL_DRIVER": "smtp",
		"STORAGE_DRIVER": "s3", "S3_SECRET_KEY": testS3Secret, "PORT": "x",
		"APP_ENV": "production", "ALLOWED_ORIGINS": "nope",
	})
	if err == nil {
		t.Fatal("expected rejection")
	}
	for _, secret := range []string{"short-secret-value", testSMTPPassword, testS3Secret, testDBSecretMarker, strongSecret} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error text leaks %q:\n%v", secret, err)
		}
	}
	err = validEnv(t, map[string]string{
		"APP_ENV": "production", "JWT_SECRET": "replace-with-at-least-32-characters-xx",
		"DB_URL": "postgres://u:" + testDBSecretMarker + "@h/d?sslmode=disable",
	})
	if err == nil || strings.Contains(err.Error(), "at-least-32-characters-xx") || strings.Contains(err.Error(), testDBSecretMarker) {
		t.Fatalf("unexpected error text: %v", err)
	}
}

func TestParseDBReportsEveryProblem(t *testing.T) {
	cleanEnv(t)
	t.Setenv("DB_MAX_IDLE_CONNS", "lots")
	t.Setenv("DB_CONN_MAX_LIFETIME", "forever")
	_, err := ParseDB()
	if err == nil {
		t.Fatal("expected rejection")
	}
	problems := problemsOf(t, err)
	for _, name := range []string{"DB_URL", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME"} {
		if _, ok := problems[name]; !ok {
			t.Errorf("problem for %s missing in %v", name, err)
		}
	}
	t.Setenv("DB_MAX_IDLE_CONNS", "5")
	t.Setenv("DB_CONN_MAX_LIFETIME", "1h")
	t.Setenv("DB_URL", "postgres://localhost/x")
	if _, err := ParseDB(); err != nil {
		t.Fatalf("valid db config rejected: %v", err)
	}
}

func TestEmptyValuesNeverWeakenSecurityCriticalSettings(t *testing.T) {
	err := validEnv(t, map[string]string{"JWT_ACCESS_TTL": "", "PORT": "", "JWT_SECRET": ""})
	if err == nil {
		t.Fatal("empty JWT_SECRET must be rejected")
	}
	problems := problemsOf(t, err)
	if _, ok := problems["JWT_SECRET"]; !ok {
		t.Fatalf("JWT_SECRET missing: %v", err)
	}
	if len(problems) != 1 {
		t.Fatalf("problems = %v", problems)
	}
}
