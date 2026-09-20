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
