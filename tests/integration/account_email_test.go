package integration

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/mail"
)

const mailTestPassword = "good-password-1"

func requestWithHeader(t *testing.T, tc *TestContext, method, path, token, header, value string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if header != "" {
		req.Header.Set(header, value)
	}
	rec := httptest.NewRecorder()
	tc.Router.ServeHTTP(rec, req)
	return rec
}

func signUp(t *testing.T, tc *TestContext, email string) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, "/api/v1/auth/register", "", map[string]any{
		"email": email, "password": mailTestPassword,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register %s: %d: %s", email, rec.Code, rec.Body.String())
	}
}

func login(t *testing.T, tc *TestContext, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, tc.Router, http.MethodPost, "/api/v1/auth/login", "", map[string]any{"email": email, "password": password})
}

func accessToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("login: %d: %s", rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)["access_token"].(string)
}

func linkToken(t *testing.T, msg mail.Message, path string) string {
	t.Helper()
	for _, field := range strings.Fields(msg.Text) {
		if strings.HasPrefix(field, "http://localhost:3000") {
			u, err := url.Parse(field)
			if err != nil {
				t.Fatalf("parse link: %v", err)
			}
			if u.Path != path {
				t.Fatalf("link path = %q, want %q", u.Path, path)
			}
			return u.Query().Get("token")
		}
	}
	t.Fatalf("no link in the message: %q", msg.Text)
	return ""
}

func only(t *testing.T, tc *TestContext, to string) mail.Message {
	t.Helper()
	msgs := tc.Mail.To(to)
	if len(msgs) != 1 {
		t.Fatalf("mails to %s = %d, want 1: %+v", to, len(msgs), msgs)
	}
	return msgs[0]
}

func profileVerifiedAt(t *testing.T, tc *TestContext, token string) any {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile: %d: %s", rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)["email_verified_at"]
}

func postAuth(t *testing.T, tc *TestContext, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, tc.Router, http.MethodPost, "/api/v1/auth/"+path, "", body)
}

func TestEmailVerification_RegisterSendsALinkThatConfirmsTheAddress(t *testing.T) {
	tc := newTestContext(t)
	signUp(t, tc, "ann@example.test")

	msg := only(t, tc, "ann@example.test")
	if msg.Subject != "Confirm your email address" || !strings.Contains(msg.HTML, "/verify-email?token=") {
		t.Fatalf("message = %+v", msg)
	}
	token := linkToken(t, msg, "/verify-email")

	var stored string
	tc.DB.Raw("SELECT token_hash FROM email_tokens WHERE kind = 'verify_email'").Scan(&stored)
	sum := sha256.Sum256([]byte(token))
	if stored == token || stored != hex.EncodeToString(sum[:]) {
		t.Fatalf("the database holds %q for token %q; only its sha256 may be stored", stored, token)
	}

	access := accessToken(t, login(t, tc, "ann@example.test", mailTestPassword))
	if profileVerifiedAt(t, tc, access) != nil {
		t.Fatal("a new account must start unverified")
	}

	if rec := postAuth(t, tc, "verify-email", map[string]any{"token": "not-a-real-token"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong token: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := postAuth(t, tc, "verify-email", map[string]any{}); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing token: got %d", rec.Code)
	}
	rec := postAuth(t, tc, "verify-email", map[string]any{"token": " " + token + " "})
	if rec.Code != http.StatusOK {
		t.Fatalf("verify: %d: %s", rec.Code, rec.Body.String())
	}
	if profileVerifiedAt(t, tc, access) == nil {
		t.Fatal("the profile does not show the confirmed address")
	}
	if rec := postAuth(t, tc, "verify-email", map[string]any{"token": token}); rec.Code != http.StatusBadRequest {
		t.Fatalf("reusing the link: expected 400, got %d", rec.Code)
	}
}

func TestEmailVerification_MailLanguageFollowsAcceptLanguage(t *testing.T) {
	tc := newTestContext(t)
	rec := requestWithHeader(t, tc, http.MethodPost, "/api/v1/auth/register", "", "Accept-Language", "ru-RU,ru;q=0.9", map[string]any{
		"email": "olga@example.test", "password": mailTestPassword,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d: %s", rec.Code, rec.Body.String())
	}
	msg := only(t, tc, "olga@example.test")
	if msg.Subject != "Подтвердите адрес электронной почты" || !strings.Contains(msg.Text, "действует 48 ч.") || strings.Contains(msg.Text, "..") {
		t.Fatalf("message = %+v", msg)
	}
}

func TestEmailVerification_ResendRulesAndExpiry(t *testing.T) {
	tc := newTestContext(t)
	signUp(t, tc, "ann@example.test")
	first := linkToken(t, only(t, tc, "ann@example.test"), "/verify-email")

	known := postAuth(t, tc, "resend-verification", map[string]any{"email": "ann@example.test"})
	unknown := postAuth(t, tc, "resend-verification", map[string]any{"email": "nobody@example.test"})
	if known.Code != http.StatusAccepted || unknown.Code != http.StatusAccepted || known.Body.String() != unknown.Body.String() {
		t.Fatalf("known: %d %s / unknown: %d %s — the answers must not reveal which address exists",
			known.Code, known.Body.String(), unknown.Code, unknown.Body.String())
	}
	if got := len(tc.Mail.To("ann@example.test")); got != 1 {
		t.Fatalf("a resend inside the cool-down sent a mail (%d in total)", got)
	}
	if got := len(tc.Mail.To("nobody@example.test")); got != 0 {
		t.Fatalf("a mail went to an address that has no account")
	}

	tc.DB.Exec("UPDATE email_tokens SET created_at = now() - interval '2 minutes'")
	if rec := postAuth(t, tc, "resend-verification", map[string]any{"email": "ANN@example.test"}); rec.Code != http.StatusAccepted {
		t.Fatalf("resend: %d", rec.Code)
	}
	msgs := tc.Mail.To("ann@example.test")
	if len(msgs) != 2 {
		t.Fatalf("mails after the cool-down = %d, want 2", len(msgs))
	}
	second := linkToken(t, msgs[1], "/verify-email")
	if second == first {
		t.Fatal("a resend must issue a new token")
	}
	if rec := postAuth(t, tc, "verify-email", map[string]any{"token": first}); rec.Code != http.StatusBadRequest {
		t.Fatalf("the replaced link still works: %d", rec.Code)
	}

	tc.DB.Exec("UPDATE email_tokens SET expires_at = now() - interval '1 minute'")
	if rec := postAuth(t, tc, "verify-email", map[string]any{"token": second}); rec.Code != http.StatusBadRequest {
		t.Fatalf("an expired link worked: %d", rec.Code)
	}

	tc.DB.Exec("UPDATE email_tokens SET expires_at = now() + interval '1 hour', created_at = now() - interval '5 minutes'")
	if rec := postAuth(t, tc, "verify-email", map[string]any{"token": second}); rec.Code != http.StatusOK {
		t.Fatalf("verify: %d", rec.Code)
	}
	tc.Mail.Reset()
	postAuth(t, tc, "resend-verification", map[string]any{"email": "ann@example.test"})
	if len(tc.Mail.Messages()) != 0 {
		t.Fatal("a verified address received another verification mail")
	}
}

func TestEmailVerification_RegistrationSurvivesAMailFailure(t *testing.T) {
	tc := newTestContext(t)
	tc.Mail.FailWith(errors.New("smtp down"))
	signUp(t, tc, "ann@example.test")
	if rec := login(t, tc, "ann@example.test", mailTestPassword); rec.Code != http.StatusOK {
		t.Fatalf("login after a failed mail: %d", rec.Code)
	}
	tc.Mail.FailWith(nil)
	postAuth(t, tc, "resend-verification", map[string]any{"email": "ann@example.test"})
	if len(tc.Mail.To("ann@example.test")) != 1 {
		t.Fatal("the user could not ask for another mail after the first one failed")
	}
}

func TestEmailVerification_LoginCanRequireAConfirmedAddress(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Mail.RequireVerifiedEmail = true })
	signUp(t, tc, "ann@example.test")

	rec := login(t, tc, "ann@example.test", mailTestPassword)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "EMAIL_NOT_VERIFIED") {
		t.Fatalf("unverified login: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := login(t, tc, "ann@example.test", "wrong-password-1"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("a wrong password must still be 401, got %d", rec.Code)
	}

	token := linkToken(t, only(t, tc, "ann@example.test"), "/verify-email")
	if rec := postAuth(t, tc, "verify-email", map[string]any{"token": token}); rec.Code != http.StatusOK {
		t.Fatalf("verify: %d", rec.Code)
	}
	if rec := login(t, tc, "ann@example.test", mailTestPassword); rec.Code != http.StatusOK {
		t.Fatalf("login after verifying: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPasswordReset_FullFlow(t *testing.T) {
	tc := newTestContext(t)
	signUp(t, tc, "ann@example.test")
	tc.Mail.Reset()

	first := login(t, tc, "ann@example.test", mailTestPassword)
	oldAccess := accessToken(t, first)
	oldRefresh := strings.SplitN(first.Header().Get("Set-Cookie"), ";", 2)[0]

	known := postAuth(t, tc, "forgot-password", map[string]any{"email": " Ann@Example.test "})
	unknown := postAuth(t, tc, "forgot-password", map[string]any{"email": "nobody@example.test"})
	if known.Code != http.StatusAccepted || known.Body.String() != unknown.Body.String() {
		t.Fatalf("answers differ: %d %s / %d %s", known.Code, known.Body.String(), unknown.Code, unknown.Body.String())
	}
	if len(tc.Mail.To("nobody@example.test")) != 0 {
		t.Fatal("a mail went to an address without an account")
	}
	msg := only(t, tc, "ann@example.test")
	if msg.Subject != "Reset your password" {
		t.Fatalf("subject = %q", msg.Subject)
	}
	token := linkToken(t, msg, "/reset-password")

	postAuth(t, tc, "forgot-password", map[string]any{"email": "ann@example.test"})
	if len(tc.Mail.To("ann@example.test")) != 1 {
		t.Fatal("a second request inside the cool-down sent another mail")
	}

	if rec := postAuth(t, tc, "reset-password", map[string]any{"token": token, "new_password": "short"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("weak password: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := postAuth(t, tc, "reset-password", map[string]any{"token": "bogus", "new_password": "a-brand-new-pass-1"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus token: expected 400, got %d", rec.Code)
	}
	if rec := postAuth(t, tc, "verify-email", map[string]any{"token": token}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a reset link must not confirm an address: %d", rec.Code)
	}

	rec := postAuth(t, tc, "reset-password", map[string]any{"token": token, "new_password": "a-brand-new-pass-1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("reset after rejected attempts (the token must survive them): %d: %s", rec.Code, rec.Body.String())
	}

	if rec := login(t, tc, "ann@example.test", mailTestPassword); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the old password still works: %d", rec.Code)
	}
	newAccess := accessToken(t, login(t, tc, "ann@example.test", "a-brand-new-pass-1"))
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", oldAccess, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the access token from before the reset still works: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", newAccess, nil); rec.Code != http.StatusOK {
		t.Fatalf("the new access token: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.Header.Set("Cookie", oldRefresh)
	refreshed := httptest.NewRecorder()
	tc.Router.ServeHTTP(refreshed, req)
	if refreshed.Code != http.StatusUnauthorized {
		t.Fatalf("the refresh token from before the reset still works: %d", refreshed.Code)
	}
	if profileVerifiedAt(t, tc, newAccess) == nil {
		t.Fatal("receiving the reset mail proves the address, so the account should now be confirmed")
	}

	if rec := postAuth(t, tc, "reset-password", map[string]any{"token": token, "new_password": "another-new-pass-2"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("reusing the link: expected 400, got %d", rec.Code)
	}
	if rec := login(t, tc, "ann@example.test", "another-new-pass-2"); rec.Code != http.StatusUnauthorized {
		t.Fatal("a reused link changed the password")
	}
}

func TestPasswordReset_ExpiryAndOtherPasswordChanges(t *testing.T) {
	tc := newTestContext(t)
	signUp(t, tc, "ann@example.test")
	access := accessToken(t, login(t, tc, "ann@example.test", mailTestPassword))
	tc.Mail.Reset()

	postAuth(t, tc, "forgot-password", map[string]any{"email": "ann@example.test"})
	expired := linkToken(t, only(t, tc, "ann@example.test"), "/reset-password")
	tc.DB.Exec("UPDATE email_tokens SET expires_at = now() - interval '1 second' WHERE kind = 'reset_password'")
	if rec := postAuth(t, tc, "reset-password", map[string]any{"token": expired, "new_password": "a-brand-new-pass-1"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("an expired link worked: %d", rec.Code)
	}

	tc.DB.Exec("UPDATE email_tokens SET created_at = now() - interval '5 minutes'")
	tc.Mail.Reset()
	postAuth(t, tc, "forgot-password", map[string]any{"email": "ann@example.test"})
	token := linkToken(t, only(t, tc, "ann@example.test"), "/reset-password")
	if rec := postAuth(t, tc, "reset-password", map[string]any{"token": expired, "new_password": "a-brand-new-pass-1"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("the replaced link still works: %d", rec.Code)
	}

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/profile/password", access, map[string]any{
		"current_password": mailTestPassword, "new_password": "changed-in-profile-1",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("change password: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := postAuth(t, tc, "reset-password", map[string]any{"token": token, "new_password": "attacker-chosen-pass"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a reset link issued before a password change still works: %d", rec.Code)
	}
	if rec := login(t, tc, "ann@example.test", "changed-in-profile-1"); rec.Code != http.StatusOK {
		t.Fatalf("login with the password set in the profile: %d", rec.Code)
	}
}

func TestAccountEmail_ValidationAndAccountDeletion(t *testing.T) {
	tc := newTestContext(t)
	for _, body := range []map[string]any{{"email": "not-an-email"}, {}, {"email": strings.Repeat("a", 300) + "@example.test"}} {
		for _, path := range []string{"forgot-password", "resend-verification"} {
			if rec := postAuth(t, tc, path, body); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
				t.Errorf("%s %v: got %d", path, body, rec.Code)
			}
		}
	}
	for _, body := range []map[string]any{{"token": "x"}, {"new_password": mailTestPassword}, {}} {
		if rec := postAuth(t, tc, "reset-password", body); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("reset-password %v: got %d", body, rec.Code)
		}
	}

	signUp(t, tc, "ann@example.test")
	access := accessToken(t, login(t, tc, "ann@example.test", mailTestPassword))
	postAuth(t, tc, "forgot-password", map[string]any{"email": "ann@example.test"})
	var before int64
	tc.DB.Table("email_tokens").Count(&before)
	if before != 2 {
		t.Fatalf("tokens = %d, want one verification and one reset token", before)
	}

	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile", access, map[string]any{"password": mailTestPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete account: %d: %s", rec.Code, rec.Body.String())
	}
	var after int64
	tc.DB.Table("email_tokens").Count(&after)
	if after != 0 {
		t.Fatalf("%d emailed tokens survived the deletion of the account", after)
	}
	tc.Mail.Reset()
	postAuth(t, tc, "forgot-password", map[string]any{"email": "ann@example.test"})
	if len(tc.Mail.Messages()) != 0 {
		t.Fatal("a deleted account can still receive reset mail")
	}
}
