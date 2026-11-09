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

func backdateRotations(t *testing.T, tc *TestContext, age string) {
	t.Helper()
	if err := tc.DB.Exec("UPDATE user_sessions SET rotated_at = now() - ?::interval WHERE rotated_at IS NOT NULL", age).Error; err != nil {
		t.Fatal(err)
	}
}

func requireRefresh(t *testing.T, tc *TestContext, name string, ck *http.Cookie, want int) *http.Cookie {
	t.Helper()
	code, next := doRefresh(tc, ck)
	if code != want {
		t.Fatalf("%s: expected %d, got %d", name, want, code)
	}
	return next
}

func TestRefreshReuse_ReplayAfterGraceRevokesTheWholeFamilyButNotOtherLogins(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "reuse@example.test")
	laptop := loginWithCookie(t, tc, "reuse@example.test", "password-123")
	phone := loginWithCookie(t, tc, "reuse@example.test", "password-123")

	first := requireRefresh(t, tc, "first rotation", laptop.refresh, http.StatusOK)
	second := requireRefresh(t, tc, "second rotation", first, http.StatusOK)
	newest := requireRefresh(t, tc, "third rotation", second, http.StatusOK)
	backdateRotations(t, tc, "5 minutes")

	requireRefresh(t, tc, "replayed token", laptop.refresh, http.StatusUnauthorized)
	for name, ck := range map[string]*http.Cookie{"newest descendant": newest, "middle descendant": second} {
		requireRefresh(t, tc, name+" after reuse detection", ck, http.StatusUnauthorized)
	}
	requireRefresh(t, tc, "another login of the same user", phone.refresh, http.StatusOK)

	var left int64
	if err := tc.DB.Table("user_sessions").Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left == 0 {
		t.Fatal("the other login must keep its session")
	}
}

func TestRefreshReuse_ReplayIsDetectedEvenWhenAnotherDeviceJustLoggedIn(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "busy@example.test")
	laptop := loginWithCookie(t, tc, "busy@example.test", "password-123")
	rotated := requireRefresh(t, tc, "rotation", laptop.refresh, http.StatusOK)
	backdateRotations(t, tc, "5 minutes")

	phone := loginWithCookie(t, tc, "busy@example.test", "password-123")

	requireRefresh(t, tc, "replayed token", laptop.refresh, http.StatusUnauthorized)
	requireRefresh(t, tc, "rotated cookie of the replayed family", rotated, http.StatusUnauthorized)
	requireRefresh(t, tc, "fresh login on another device", phone.refresh, http.StatusOK)
}

func TestRefreshReuse_ConcurrentRefreshRaceIsTolerated(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "race@example.test")
	laptop := loginWithCookie(t, tc, "race@example.test", "password-123")
	phone := loginWithCookie(t, tc, "race@example.test", "password-123")

	winner := requireRefresh(t, tc, "first refresh", laptop.refresh, http.StatusOK)
	requireRefresh(t, tc, "second (racing) refresh", laptop.refresh, http.StatusUnauthorized)
	requireRefresh(t, tc, "the cookie the winner received", winner, http.StatusOK)
	requireRefresh(t, tc, "other device after a race", phone.refresh, http.StatusOK)
}

func TestRefreshReuse_ReplayJustInsideTheGraceKeepsTheFamily(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "grace@example.test")
	laptop := loginWithCookie(t, tc, "grace@example.test", "password-123")
	rotated := requireRefresh(t, tc, "rotation", laptop.refresh, http.StatusOK)
	backdateRotations(t, tc, "5 seconds")

	requireRefresh(t, tc, "replay inside the grace window", laptop.refresh, http.StatusUnauthorized)
	requireRefresh(t, tc, "descendant stays valid", rotated, http.StatusOK)

	backdateRotations(t, tc, "30 seconds")
	requireRefresh(t, tc, "replay just after the grace window", laptop.refresh, http.StatusUnauthorized)
}

func TestRefreshReuse_ReplayOfALoggedOutCookieDoesNotTouchOtherLogins(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "logout@example.test")
	laptop := loginWithCookie(t, tc, "logout@example.test", "password-123")
	phone := loginWithCookie(t, tc, "logout@example.test", "password-123")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(laptop.refresh)
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d: %s", rec.Code, rec.Body.String())
	}

	requireRefresh(t, tc, "replay of the logged-out cookie", laptop.refresh, http.StatusUnauthorized)
	requireRefresh(t, tc, "other login", phone.refresh, http.StatusOK)
}

func TestRefreshReuse_LogoutEndsTheWholeFamily(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "family-logout@example.test")
	laptop := loginWithCookie(t, tc, "family-logout@example.test", "password-123")
	rotated := requireRefresh(t, tc, "rotation", laptop.refresh, http.StatusOK)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.AddCookie(rotated)
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)

	var left int64
	if err := tc.DB.Table("user_sessions").Count(&left).Error; err != nil {
		t.Fatal(err)
	}
	if left != 0 {
		t.Fatalf("logout left %d sessions of the family behind", left)
	}
}

func TestRefreshReuse_RotatedRecordsAreForgottenAfterTheRetention(t *testing.T) {
	tc := newTestContext(t)
	registerForReuse(t, tc, "retention@example.test")
	laptop := loginWithCookie(t, tc, "retention@example.test", "password-123")
	first := requireRefresh(t, tc, "first rotation", laptop.refresh, http.StatusOK)
	backdateRotations(t, tc, "72 hours")
	second := requireRefresh(t, tc, "second rotation", first, http.StatusOK)

	var rotated int64
	if err := tc.DB.Table("user_sessions").Where("rotated_at IS NOT NULL").Count(&rotated).Error; err != nil {
		t.Fatal(err)
	}
	if rotated != 1 {
		t.Fatalf("rotated records = %d, want only the fresh one", rotated)
	}
	requireRefresh(t, tc, "ancient replay is just unknown", laptop.refresh, http.StatusUnauthorized)
	requireRefresh(t, tc, "descendant is not affected by an unknown token", second, http.StatusOK)
}
