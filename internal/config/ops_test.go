package config

import (
	"runtime"
	"testing"
	"time"
)

func loadWith(t *testing.T, overrides map[string]string) (*Config, error) {
	t.Helper()
	if err := validEnv(t, overrides); err != nil {
		return nil, err
	}
	return Load()
}

func TestOperationalDefaults(t *testing.T) {
	cfg, err := loadWith(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Server.Proxies()) != 0 {
		t.Fatalf("no proxy is trusted by default, got %v", cfg.Server.Proxies())
	}
	if cfg.Server.ShutdownTimeout != 30*time.Second {
		t.Fatalf("shutdown timeout = %v", cfg.Server.ShutdownTimeout)
	}
	db := cfg.DB
	if db.StatementTimeout != 30*time.Second || db.LockTimeout != 5*time.Second || db.IdleInTxTimeout != time.Minute ||
		db.ConnMaxIdleTime != 10*time.Minute || db.SlowQueryThreshold != 200*time.Millisecond || db.MaxIdleConnections != 10 {
		t.Fatalf("database defaults = %+v", db)
	}
	if cfg.Auth.HashSlots() != runtime.NumCPU() || cfg.Auth.LoginFailureLimit != 5 {
		t.Fatalf("auth defaults = %+v", cfg.Auth)
	}
}

func TestEmptyTrustedProxiesEntryIsIgnored(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{"TRUSTED_PROXIES": " , 10.0.0.1 ,"})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Server.Proxies(); len(got) != 1 || got[0] != "10.0.0.1" {
		t.Fatalf("proxies = %v", got)
	}
}

func TestOperationalSettingsAreValidated(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"trust everyone ipv4", map[string]string{"TRUSTED_PROXIES": "0.0.0.0/0"}, "TRUSTED_PROXIES"},
		{"trust everyone ipv6", map[string]string{"TRUSTED_PROXIES": "10.0.0.1,::/0"}, "TRUSTED_PROXIES"},
		{"idle above open", map[string]string{"DB_MAX_IDLE_CONNS": "30", "DB_MAX_OPEN_CONNS": "10"}, "DB_MAX_IDLE_CONNS"},
		{"lock above statement", map[string]string{"DB_STATEMENT_TIMEOUT": "2s", "DB_LOCK_TIMEOUT": "5s"}, "DB_LOCK_TIMEOUT"},
		{"negative statement timeout", map[string]string{"DB_STATEMENT_TIMEOUT": "-1s"}, "DB_STATEMENT_TIMEOUT"},
		{"bad duration", map[string]string{"DB_CONN_MAX_IDLE_TIME": "soon"}, "DB_CONN_MAX_IDLE_TIME"},
		{"zero shutdown timeout", map[string]string{"SHUTDOWN_TIMEOUT": "0s"}, "SHUTDOWN_TIMEOUT"},
		{"negative hash concurrency", map[string]string{"AUTH_HASH_CONCURRENCY": "-1"}, "AUTH_HASH_CONCURRENCY"},
		{"huge hash concurrency", map[string]string{"AUTH_HASH_CONCURRENCY": "100000"}, "AUTH_HASH_CONCURRENCY"},
		{"lock without base", map[string]string{"AUTH_LOGIN_LOCK_BASE": "0s"}, "AUTH_LOGIN_LOCK_BASE"},
		{"max below base", map[string]string{"AUTH_LOGIN_LOCK_BASE": "1m", "AUTH_LOGIN_LOCK_MAX": "10s"}, "AUTH_LOGIN_LOCK_MAX"},
		{"zero retention interval", map[string]string{"RETENTION_INTERVAL": "0s"}, "RETENTION_INTERVAL"},
		{"negative outbox retention", map[string]string{"RETENTION_OUTBOX_DONE": "-1h"}, "RETENTION_OUTBOX_DONE"},
		{"unknown rate limit store", map[string]string{"RATE_LIMIT_STORE": "redis"}, "RATE_LIMIT_STORE"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validEnv(t, tc.env)
			if err == nil {
				t.Fatal("expected the configuration to be rejected")
			}
			if _, ok := problemsOf(t, err)[tc.want]; !ok {
				t.Fatalf("no problem for %s in %v", tc.want, err)
			}
		})
	}
}

func TestOperationalSettingsAcceptedValues(t *testing.T) {
	cases := map[string]map[string]string{
		"lock disabled":      {"AUTH_LOGIN_FAILURE_LIMIT": "0", "AUTH_LOGIN_LOCK_BASE": "0s", "AUTH_LOGIN_LOCK_MAX": "0s"},
		"timeouts disabled":  {"DB_STATEMENT_TIMEOUT": "0s", "DB_LOCK_TIMEOUT": "0s", "DB_IDLE_IN_TX_TIMEOUT": "0s", "DB_CONN_MAX_IDLE_TIME": "0s"},
		"explicit hashing":   {"AUTH_HASH_CONCURRENCY": "2", "AUTH_HASH_WAIT": "0s"},
		"long shutdown":      {"SHUTDOWN_TIMEOUT": "2m"},
		"narrow proxy range": {"TRUSTED_PROXIES": "172.18.0.0/16"},
	}
	for name, env := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validEnv(t, env); err != nil {
				t.Fatalf("rejected: %v", err)
			}
		})
	}
}

func TestWideProxyRangeDetection(t *testing.T) {
	cases := map[string]bool{
		"172.16.0.0/12": true, "10.0.0.0/8": true, "172.18.0.0/16": false, "10.1.2.3": false,
		"fd00::/8": true, "2001:db8::/48": false, "garbage": false,
	}
	for entry, want := range cases {
		if got := wideProxyRange(entry); got != want {
			t.Errorf("wideProxyRange(%q) = %v, want %v", entry, got, want)
		}
	}
}

func TestDBOptionsCarryEveryLimit(t *testing.T) {
	cfg, err := loadWith(t, map[string]string{"DB_STATEMENT_TIMEOUT": "9s", "DB_SLOW_QUERY_THRESHOLD": "75ms"})
	if err != nil {
		t.Fatal(err)
	}
	opts := cfg.DB.Options("staging")
	if opts.Environment != "staging" || opts.StatementTimeout != 9*time.Second || opts.SlowQueryThreshold != 75*time.Millisecond ||
		opts.LockTimeout != 5*time.Second || opts.IdleInTxnTimeout != time.Minute || opts.MaxOpenConnections != 25 {
		t.Fatalf("options = %+v", opts)
	}
}
