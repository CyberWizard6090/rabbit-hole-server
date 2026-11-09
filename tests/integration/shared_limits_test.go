package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/mail"
)

func newReplica(t *testing.T, tc *TestContext) (*gin.Engine, *app.Container) {
	t.Helper()
	cfg := tc.Config
	container, err := app.NewContainerWithMailer(integrationDB, &cfg, mail.NewMemory())
	if err != nil {
		t.Fatalf("create a replica: %v", err)
	}
	return app.SetupRouter(container, &cfg), container
}

func badLogin(t *testing.T, router http.Handler, email string) int {
	t.Helper()
	return request(t, router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": email, "password": "wrong-password-1",
	}).Code
}

func TestSharedLimits_ARequestBudgetIsSharedBetweenReplicas(t *testing.T) {
	tc := newTestContext(t)
	a, _ := newReplica(t, tc)
	b, _ := newReplica(t, tc)

	for i := 0; i < 10; i++ {
		router := http.Handler(a)
		if i%2 == 1 {
			router = b
		}
		if code := badLogin(t, router, "nobody-"+itoa(uint(i))+"@example.test"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i, code)
		}
	}
	if code := badLogin(t, b, "nobody-last@example.test"); code != http.StatusTooManyRequests {
		t.Fatalf("the 11th attempt on the other replica = %d, want 429", code)
	}
	restarted, _ := newReplica(t, tc)
	if code := badLogin(t, restarted, "nobody-after@example.test"); code != http.StatusTooManyRequests {
		t.Fatalf("a restarted replica forgot the budget: %d", code)
	}
}

func TestSharedLimits_AMemoryStoreCountsPerProcess(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.RateLimit.Store = "memory" })
	a, _ := newReplica(t, tc)
	b, _ := newReplica(t, tc)
	for i := 0; i < 10; i++ {
		if code := badLogin(t, a, "mem-"+itoa(uint(i))+"@example.test"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d", i, code)
		}
	}
	if code := badLogin(t, a, "mem-last@example.test"); code != http.StatusTooManyRequests {
		t.Fatalf("the same replica must be limited: %d", code)
	}
	if code := badLogin(t, b, "mem-other@example.test"); code != http.StatusUnauthorized {
		t.Fatalf("a memory store is per process, the other replica answered %d", code)
	}
}

func TestSharedLimits_ASignInLockIsSharedBetweenReplicas(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Auth.LoginFailureLimit = 2 })
	email := "lock-shared@example.test"
	_, _ = newUser(t, tc, email)
	a, _ := newReplica(t, tc)
	b, _ := newReplica(t, tc)

	for i := 0; i < 2; i++ {
		if code := badLogin(t, a, email); code != http.StatusUnauthorized {
			t.Fatalf("failure %d = %d", i, code)
		}
	}
	rec := request(t, b, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": "password-123"})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("the right password was accepted while the account is locked on another replica: %d", rec.Code)
	}
	var locked int64
	if err := tc.DB.Raw("SELECT count(*) FROM login_lockouts WHERE locked_until > now()").Scan(&locked).Error; err != nil || locked != 1 {
		t.Fatalf("lockout rows = %d, %v", locked, err)
	}
}

func TestSharedLimits_ExpiredCountersArePurged(t *testing.T) {
	tc := newTestContext(t)
	_, container := newReplica(t, tc)
	if err := tc.DB.Exec("INSERT INTO rate_limit_windows (bucket, key, count, reset_at) VALUES ('old', 'x', 3, now() - interval '1 hour')").Error; err != nil {
		t.Fatal(err)
	}
	if err := tc.DB.Exec("INSERT INTO login_lockouts (subject, failures, last_fail_at) VALUES ('stale', 3, now() - interval '3 hours')").Error; err != nil {
		t.Fatal(err)
	}
	if err := tc.DB.Exec("INSERT INTO rate_limit_windows (bucket, key, count, reset_at) VALUES ('live', 'x', 1, now() + interval '1 hour')").Error; err != nil {
		t.Fatal(err)
	}
	if container.PurgeRateLimits == nil {
		t.Fatal("the postgres store must provide a purge")
	}
	if err := container.PurgeRateLimits(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, tc, "SELECT count(*) FROM rate_limit_windows"); n != 1 {
		t.Fatalf("windows left = %d, want only the live one", n)
	}
	if n := countRows(t, tc, "SELECT count(*) FROM login_lockouts"); n != 0 {
		t.Fatalf("stale lockouts left = %d", n)
	}
}
