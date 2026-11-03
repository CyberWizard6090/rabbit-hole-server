package integration

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTP_OversizedBodyIsRejected(t *testing.T) {
	tc := newTestContext(t)

	body := `{"email":"big@example.test","password":"` + strings.Repeat("a", 2<<20) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: expected 413, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHTTP_LogoutWorksWithoutAccessToken(t *testing.T) {
	tc := newTestContext(t)

	email := "logout-no-access-" + itoa(uint(time.Now().UnixNano()%1_000_000)) + "@example.test"
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": "password-123",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": email, "password": "password-123",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d: %s", rec.Code, rec.Body.String())
	}

	var refresh *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == "refresh_token" {
			refresh = ck
		}
	}
	if refresh == nil {
		t.Fatal("login did not set refresh_token cookie")
	}

	// No Authorization header: the access token may already be expired.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(refresh)
	out := httptest.NewRecorder()
	tc.Router.ServeHTTP(out, req)
	if out.Code != http.StatusOK {
		t.Fatalf("logout without access token: expected 200, got %d: %s", out.Code, out.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(refresh)
	out = httptest.NewRecorder()
	tc.Router.ServeHTTP(out, req)
	if out.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: expected 401, got %d", out.Code)
	}
}

func TestHTTP_AuthRateLimitsAreIndependent(t *testing.T) {
	tc := newTestContext(t)

	// Exhaust the login budget (10/min).
	for i := 0; i < 10; i++ {
		request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
			"email": "nobody@example.test", "password": "wrong-password",
		})
	}
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{
		"email": "nobody@example.test", "password": "wrong-password",
	})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login #11: expected 429, got %d", rec.Code)
	}

	// Refresh has its own budget: it answers 401 (no cookie), not 429.
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/auth/refresh", "", nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh after login limit: expected 401, got %d", rec.Code)
	}
}
