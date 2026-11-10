package config

import "testing"

func TestRehostLimitDefaultsToTheImageLimit(t *testing.T) {
	baseEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Storage.RehostLimit(); got != 10<<20 {
		t.Fatalf("default rehost limit = %d", got)
	}
	t.Setenv("REHOST_MAX_BYTES", "2048")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := cfg.Storage.RehostLimit(); got != 2048 {
		t.Fatalf("rehost limit = %d", got)
	}
	t.Setenv("REHOST_MAX_BYTES", "-1")
	if _, err := Load(); err == nil {
		t.Fatal("a negative REHOST_MAX_BYTES must be rejected")
	}
}
