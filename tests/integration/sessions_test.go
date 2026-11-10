package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const (
	chromeOnWindows = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"
	safariOnIPhone  = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1"
)

type signedIn struct {
	token  string
	cookie string
}

// signInFrom signs the user in as a client with the given User-Agent and address.
func signInFrom(t *testing.T, tc *TestContext, email, userAgent, ip string) signedIn {
	t.Helper()
	forgetAuthBudgets(t, tc)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		strings.NewReader(fmt.Sprintf(`{"email":%q,"password":"password-123"}`, email)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.RemoteAddr = ip + ":4000"
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign in from %q: %d: %s", userAgent, rec.Code, rec.Body.String())
	}
	data := decodeJSON(t, rec)["data"].(map[string]any)
	cookie := strings.SplitN(rec.Header().Get("Set-Cookie"), ";", 2)[0]
	return signedIn{token: data["access_token"].(string), cookie: cookie}
}

func refreshWith(t *testing.T, tc *TestContext, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.Header.Set("Cookie", cookie)
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return rec
}

func listSessions(t *testing.T, tc *TestContext, token string) []map[string]any {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile/sessions", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sessions: %d: %s", rec.Code, rec.Body.String())
	}
	var out []map[string]any
	for _, item := range decodeJSON(t, rec)["data"].([]any) {
		out = append(out, item.(map[string]any))
	}
	return out
}

func sessionBySubstring(sessions []map[string]any, device string) map[string]any {
	for _, s := range sessions {
		if strings.Contains(s["device"].(string), device) {
			return s
		}
	}
	return nil
}

func noticesTo(tc *TestContext, email, subjectPart string) int {
	n := 0
	for _, m := range tc.Mail.To(email) {
		if strings.Contains(m.Subject, subjectPart) {
			n++
		}
	}
	return n
}

func newSessionUser(t *testing.T, tc *TestContext) (string, uint) {
	t.Helper()
	email := fmt.Sprintf("sess-%d@example.test", time.Now().UnixNano())
	id, _ := newUser(t, tc, email)
	return email, id
}

func TestSessions_ListShowsEveryDeviceAndMarksTheCurrentOne(t *testing.T) {
	tc := newTestContext(t)
	email, _ := newSessionUser(t, tc)
	desktop := signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	phone := signInFrom(t, tc, email, safariOnIPhone, "198.51.100.9")

	sessions := listSessions(t, tc, desktop.token)
	// newUser signed in once without a User-Agent, then the two sign-ins above.
	if len(sessions) != 3 {
		t.Fatalf("sessions = %d, want 3: %v", len(sessions), sessions)
	}
	chrome := sessionBySubstring(sessions, "Chrome on Windows")
	safari := sessionBySubstring(sessions, "Safari on iOS")
	if chrome == nil || safari == nil {
		t.Fatalf("device labels missing: %v", sessions)
	}
	if chrome["current"] != true || safari["current"] != false {
		t.Fatalf("current flags wrong: chrome=%v safari=%v", chrome["current"], safari["current"])
	}
	if chrome["ip"] != "203.0.113.7" || chrome["user_agent"] != chromeOnWindows {
		t.Fatalf("session did not remember the client: %v", chrome)
	}
	if again := sessionBySubstring(listSessions(t, tc, phone.token), "Safari on iOS"); again["current"] != true {
		t.Fatalf("the phone must see itself as current: %v", again)
	}
	for _, s := range sessions {
		if _, ok := s["token_hash"]; ok {
			t.Fatalf("a session leaks token material: %v", s)
		}
	}
}

func TestSessions_RefreshKeepsTheSessionAndItsDevice(t *testing.T) {
	tc := newTestContext(t)
	email, _ := newSessionUser(t, tc)
	desktop := signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	before := sessionBySubstring(listSessions(t, tc, desktop.token), "Chrome")

	rec := refreshWith(t, tc, desktop.cookie)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d: %s", rec.Code, rec.Body.String())
	}
	newToken := decodeJSON(t, rec)["data"].(map[string]any)["access_token"].(string)

	after := sessionBySubstring(listSessions(t, tc, newToken), "Chrome")
	if after == nil || after["id"] != before["id"] || after["current"] != true {
		t.Fatalf("refresh changed the session: before=%v after=%v", before, after)
	}
	if after["user_agent"] != chromeOnWindows || after["created_at"] != before["created_at"] {
		t.Fatalf("refresh lost what identifies the device: before=%v after=%v", before, after)
	}
	if count := len(listSessions(t, tc, newToken)); count != 2 {
		t.Fatalf("refresh must not add a session, have %d", count)
	}
}

func TestSessions_RevokingADeviceEndsItsAccessAndRefresh(t *testing.T) {
	tc := newTestContext(t)
	email, _ := newSessionUser(t, tc)
	desktop := signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	phone := signInFrom(t, tc, email, safariOnIPhone, "198.51.100.9")
	phoneID := sessionBySubstring(listSessions(t, tc, desktop.token), "Safari")["id"].(string)

	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/sessions/"+phoneID, desktop.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", phone.token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the revoked device's access token still works: %d", rec.Code)
	}
	if rec := refreshWith(t, tc, phone.cookie); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the revoked device can still refresh: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", desktop.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("ending another device signed this one out: %d", rec.Code)
	}
	if sessionBySubstring(listSessions(t, tc, desktop.token), "Safari") != nil {
		t.Fatal("the revoked session is still listed")
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/sessions/"+phoneID, desktop.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("revoking twice: %d, want 404", rec.Code)
	}
}

func TestSessions_AUserCannotEndSomeoneElsesSession(t *testing.T) {
	tc := newTestContext(t)
	email, _ := newSessionUser(t, tc)
	victim := signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	victimSession := sessionBySubstring(listSessions(t, tc, victim.token), "Chrome")["id"].(string)

	_, attackerToken := newUser(t, tc, fmt.Sprintf("attacker-%d@example.test", time.Now().UnixNano()))
	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/sessions/"+victimSession, attackerToken, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("foreign session: %d, want 404 indistinguishable from a missing one: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", victim.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("the victim was signed out: %d", rec.Code)
	}
	if got := len(listSessions(t, tc, attackerToken)); got != 1 {
		t.Fatalf("the attacker sees %d sessions, want only their own", got)
	}
}

func TestSessions_EndingTheCurrentOneClearsTheCookie(t *testing.T) {
	tc := newTestContext(t)
	email, _ := newSessionUser(t, tc)
	desktop := signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	id := sessionBySubstring(listSessions(t, tc, desktop.token), "Chrome")["id"].(string)
	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/sessions/"+id, desktop.token, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Header().Get("Set-Cookie"), "Max-Age=0") && !strings.Contains(rec.Header().Get("Set-Cookie"), "refresh_token=;") {
		t.Fatalf("revoking the current session: %d, cookie %q", rec.Code, rec.Header().Get("Set-Cookie"))
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", desktop.token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the ended session still works: %d", rec.Code)
	}
}

func TestSessions_SignOutEverywhereElseKeepsTheCurrentDevice(t *testing.T) {
	tc := newTestContext(t)
	email, _ := newSessionUser(t, tc)
	desktop := signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	phone := signInFrom(t, tc, email, safariOnIPhone, "198.51.100.9")

	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/sessions", desktop.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign out others: %d: %s", rec.Code, rec.Body.String())
	}
	if ended := decodeJSON(t, rec)["data"].(map[string]any)["ended"]; ended != float64(2) {
		t.Fatalf("ended = %v, want 2 (the first sign-in and the phone)", ended)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", phone.token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the phone is still signed in: %d", rec.Code)
	}
	if rec := refreshWith(t, tc, desktop.cookie); rec.Code != http.StatusOK {
		t.Fatalf("the current device lost its session: %d", rec.Code)
	}
	if got := listSessions(t, tc, desktop.token); len(got) != 1 || got[0]["current"] != true {
		t.Fatalf("sessions after sign-out-others: %v", got)
	}
}

func TestSessions_AnUnfamiliarDeviceIsAnnouncedByMail(t *testing.T) {
	tc := newTestContext(t)
	email, _ := newSessionUser(t, tc)

	signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	if n := noticesTo(tc, email, "New sign-in"); n != 1 {
		t.Fatalf("a sign-in from a new device sent %d notices, want 1", n)
	}
	notice := tc.Mail.To(email)[len(tc.Mail.To(email))-1]
	for _, want := range []string{"Chrome on Windows", "203.0.113.x"} {
		if !strings.Contains(notice.Text, want) {
			t.Fatalf("the notice lacks %q: %s", want, notice.Text)
		}
	}
	if strings.Contains(notice.Text, "203.0.113.7") {
		t.Fatalf("the notice shows the full address: %s", notice.Text)
	}

	signInFrom(t, tc, email, chromeOnWindows, "203.0.113.99") // the same device again, other address
	signInFrom(t, tc, email, strings.Replace(chromeOnWindows, "126.0", "127.0", 1), "203.0.113.7")
	if n := noticesTo(tc, email, "New sign-in"); n != 1 {
		t.Fatalf("a known device (even after a browser update) must not be announced again: %d notices", n)
	}

	signInFrom(t, tc, email, safariOnIPhone, "198.51.100.9")
	if n := noticesTo(tc, email, "New sign-in"); n != 2 {
		t.Fatalf("a second new device sent %d notices in total, want 2", n)
	}
}

func TestSessions_TheVeryFirstDeviceIsNotAnnounced(t *testing.T) {
	tc := newTestContext(t)
	email := fmt.Sprintf("first-%d@example.test", time.Now().UnixNano())
	forgetAuthBudgets(t, tc)
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": "password-123",
	}); rec.Code != http.StatusCreated {
		t.Fatalf("register: %d: %s", rec.Code, rec.Body.String())
	}
	signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	if n := noticesTo(tc, email, "New sign-in"); n != 0 {
		t.Fatalf("the first sign-in of an account sent %d notices, want none", n)
	}
}

func TestSessions_ChangingThePasswordIsAnnouncedAndEndsOtherDevices(t *testing.T) {
	tc := newTestContext(t)
	email, _ := newSessionUser(t, tc)
	desktop := signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	phone := signInFrom(t, tc, email, safariOnIPhone, "198.51.100.9")

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/profile/password", desktop.token, map[string]any{
		"current_password": "password-123", "new_password": "another-password-9",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d: %s", rec.Code, rec.Body.String())
	}
	if n := noticesTo(tc, email, "password was changed"); n != 1 {
		t.Fatalf("password change sent %d notices, want 1", n)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", phone.token, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("another device survived the password change: %d", rec.Code)
	}
	newToken := decodeJSON(t, rec)["data"].(map[string]any)["access_token"].(string)
	if got := listSessions(t, tc, newToken); len(got) != 1 || got[0]["current"] != true {
		t.Fatalf("after the change only this device remains: %v", got)
	}
}

func TestSessions_RoutesRequireSignIn(t *testing.T) {
	tc := newTestContext(t)
	for _, c := range [][2]string{
		{http.MethodGet, "/api/v1/profile/sessions"},
		{http.MethodDelete, "/api/v1/profile/sessions"},
		{http.MethodDelete, "/api/v1/profile/sessions/abc"},
	} {
		if rec := request(t, tc.Router, c[0], c[1], "", nil); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s %s without a token: %d, want 401", c[0], c[1], rec.Code)
		}
	}
}
