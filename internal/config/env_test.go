package config

import "testing"

func TestLoadEnvValidatesRequiredValues(t *testing.T) {
	t.Setenv("DB_URL", "postgres://localhost/rabbit_hole")
	t.Setenv("JWT_SECRET", "short-secret")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")

	if _, err := Load(); err == nil {
		t.Fatal("expected short JWT_SECRET to be rejected")
	}
}

func TestLoadEnvAcceptsValidValues(t *testing.T) {
	t.Setenv("DB_URL", "postgres://localhost/rabbit_hole")
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000")

	env, err := Load()
	if err != nil {
		t.Fatalf("expected valid environment, got error: %v", err)
	}
	if len(env.AllowedOrigins) != 2 || env.AllowedOrigins[0] != "http://localhost:3000" {
		t.Fatalf("unexpected allowed origins: %v", env.AllowedOrigins)
	}
	if env.JWT.AccessTTL.String() != "1h0m0s" {
		t.Fatalf("unexpected JWT access TTL: %s", env.JWT.AccessTTL)
	}
	if env.JWT.RefreshTTL.String() != "168h0m0s" {
		t.Fatalf("unexpected JWT refresh TTL: %s", env.JWT.RefreshTTL)
	}
}

const strongSecret = "k7Qz9xLm2VbN4pRt8WdY1sFgH6jCaE3u" // 29 distinct characters

func deployedEnv(t *testing.T, appEnv, secret, origins, dbURL string) error {
	t.Helper()
	t.Setenv("APP_ENV", appEnv)
	t.Setenv("JWT_SECRET", secret)
	t.Setenv("ALLOWED_ORIGINS", origins)
	t.Setenv("DB_URL", dbURL)
	t.Setenv("MAIL_DRIVER", "smtp")
	t.Setenv("SMTP_HOST", "smtp.example.com")
	t.Setenv("FRONTEND_URL", "https://app.example.com")
	_, err := Load()
	return err
}

func TestDeployedEnvironmentsRejectUnsafeConfiguration(t *testing.T) {
	const db = "postgres://app@db/rabbit_hole?sslmode=require"
	cases := []struct {
		name    string
		secret  string
		origins string
	}{
		{"example placeholder secret", "replace-with-at-least-32-characters", "https://app.example.org"},
		{"change-me secret", "change-me-change-me-change-me-change-me", "https://app.example.org"},
		{"low-entropy secret", "abababababababababababababababab", "https://app.example.org"},
		{"http origin", strongSecret, "http://app.example.org"},
		{"one of several origins is http", strongSecret, "https://app.example.org,http://other.example.org"},
	}
	for _, env := range []string{"production", "staging"} {
		for _, tc := range cases {
			t.Run(env+"/"+tc.name, func(t *testing.T) {
				if err := deployedEnv(t, env, tc.secret, tc.origins, db); err == nil {
					t.Fatal("expected the unsafe configuration to be rejected")
				}
			})
		}
	}
}

func TestDeployedEnvironmentsAcceptSafeConfiguration(t *testing.T) {
	for _, env := range []string{"production", "staging"} {
		if err := deployedEnv(t, env, strongSecret, "https://app.example.org", "postgres://app@db/rabbit_hole?sslmode=require"); err != nil {
			t.Fatalf("%s: safe configuration rejected: %v", env, err)
		}
	}
}

func TestLocalEnvironmentsKeepDevelopmentDefaults(t *testing.T) {
	for _, env := range []string{"development", "test"} {
		if err := deployedEnv(t, env, "replace-with-at-least-32-characters", "http://127.0.0.1:3000", "postgres://localhost/x?sslmode=disable"); err != nil {
			t.Fatalf("%s: %v", env, err)
		}
	}
}

func TestSecureCookiesFollowDeployedEnvironments(t *testing.T) {
	for env, want := range map[string]bool{"production": true, "staging": true, "development": false, "test": false} {
		if got := (&Config{Environment: env}).SecureCookies(); got != want {
			t.Errorf("SecureCookies(%s) = %t, want %t", env, got, want)
		}
	}
}

func TestPoolDefaultsStayBelowPostgresDefaultMaxConnections(t *testing.T) {
	t.Setenv("APP_ENV", "test")
	t.Setenv("DB_URL", "postgres://localhost/x")
	t.Setenv("JWT_SECRET", strongSecret)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DB.MaxOpenConnections > 50 {
		t.Fatalf("default max open connections = %d, want well below 100", cfg.DB.MaxOpenConnections)
	}
}
