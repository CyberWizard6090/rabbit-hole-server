package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type tokenSession struct {
	access  string
	refresh *http.Cookie
}

func loginWithCookie(t *testing.T, tc *TestContext, email, password string) tokenSession {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": password})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d: %s", rec.Code, rec.Body.String())
	}
	s := tokenSession{access: dataObject(t, rec)["access_token"].(string)}
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "refresh_token" {
			s.refresh = ck
		}
	}
	if s.refresh == nil {
		t.Fatal("no refresh cookie")
	}
	return s
}

func doRefresh(tc *TestContext, ck *http.Cookie) (int, *http.Cookie) {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.Name == "refresh_token" && c.Value != "" {
			return rec.Code, c
		}
	}
	return rec.Code, nil
}

func registerForReuse(t *testing.T, tc *TestContext, email string) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{"email": email, "password": "password-123"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestRefreshReuse_ReplayedTokenRevokesAllSessions(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "reuse@example.test")
	laptop := loginWithCookie(t, tc, "reuse@example.test", "password-123")
	phone := loginWithCookie(t, tc, "reuse@example.test", "password-123")

	code, rotated := doRefresh(tc, laptop.refresh)
	if code != http.StatusOK || rotated == nil {
		t.Fatalf("legitimate refresh: %d", code)
	}

	if err := tc.DB.Exec("UPDATE user_sessions SET created_at = now() - interval '5 minutes'").Error; err != nil {
		t.Fatal(err)
	}

	if code, _ := doRefresh(tc, laptop.refresh); code != http.StatusUnauthorized {
		t.Fatalf("replayed token: expected 401, got %d", code)
	}
	for name, ck := range map[string]*http.Cookie{"phone": phone.refresh, "rotated laptop token": rotated} {
		if code, _ := doRefresh(tc, ck); code != http.StatusUnauthorized {
			t.Errorf("%s after reuse detection: expected 401 (all sessions revoked), got %d", name, code)
		}
	}
}

func TestRefreshReuse_ConcurrentRefreshRaceIsTolerated(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "race@example.test")
	laptop := loginWithCookie(t, tc, "race@example.test", "password-123")
	phone := loginWithCookie(t, tc, "race@example.test", "password-123")

	if code, _ := doRefresh(tc, laptop.refresh); code != http.StatusOK {
		t.Fatalf("first refresh: %d", code)
	}
	if code, _ := doRefresh(tc, laptop.refresh); code != http.StatusUnauthorized {
		t.Fatalf("second (racing) refresh: expected 401, got %d", code)
	}
	if code, _ := doRefresh(tc, phone.refresh); code != http.StatusOK {
		t.Fatalf("other device must stay signed in after a race, got %d", code)
	}
}
