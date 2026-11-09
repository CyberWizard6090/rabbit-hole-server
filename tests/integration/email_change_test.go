package integration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"rabbit-hole-server/internal/service"
)

const changePath = "/api/v1/profile/email"

type changeAccount struct {
	email  string
	access string
}

func newChangeAccount(t *testing.T, tc *TestContext, email string) changeAccount {
	t.Helper()
	signUp(t, tc, email)
	acc := changeAccount{email: email, access: accessToken(t, login(t, tc, email, mailTestPassword))}
	tc.Mail.Reset()
	return acc
}

func requestChange(t *testing.T, tc *TestContext, access, newEmail, password string) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, tc.Router, http.MethodPost, changePath, access, map[string]any{"new_email": newEmail, "password": password})
}

func confirmChange(t *testing.T, tc *TestContext, token string) *httptest.ResponseRecorder {
	t.Helper()
	return postAuth(t, tc, "confirm-email-change", map[string]any{"token": token})
}

func startChange(t *testing.T, tc *TestContext, acc changeAccount, newEmail string) string {
	t.Helper()
	rec := requestChange(t, tc, acc.access, newEmail, mailTestPassword)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request change: %d: %s", rec.Code, rec.Body.String())
	}
	return linkToken(t, only(t, tc, newEmail), "/confirm-email-change")
}

func profileField(t *testing.T, tc *TestContext, access, field string) any {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", access, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("profile: %d: %s", rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)[field]
}

func TestEmailChange_FullFlow(t *testing.T) {
	tc := newTestContext(t)
	acc := newChangeAccount(t, tc, "ann@example.test")
	first := login(t, tc, acc.email, mailTestPassword)
	refresh := strings.SplitN(first.Header().Get("Set-Cookie"), ";", 2)[0]
	forgot := postAuth(t, tc, "forgot-password", map[string]any{"email": acc.email})
	if forgot.Code != http.StatusAccepted {
		t.Fatalf("forgot: %d", forgot.Code)
	}
	resetToken := linkToken(t, only(t, tc, acc.email), "/reset-password")
	tc.Mail.Reset()

	rec := requestWithHeader(t, tc, http.MethodPost, changePath, acc.access, "Accept-Language", "ru", map[string]any{
		"new_email": " New@Example.test ", "password": mailTestPassword,
	})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("request: %d: %s", rec.Code, rec.Body.String())
	}
	msg := only(t, tc, "new@example.test")
	if msg.Subject != "Подтвердите новый адрес электронной почты" || len(tc.Mail.Messages()) != 1 {
		t.Fatalf("mails = %+v", tc.Mail.Messages())
	}
	token := linkToken(t, msg, "/confirm-email-change")
	if profileField(t, tc, acc.access, "email") != acc.email || profileField(t, tc, acc.access, "pending_email") != "new@example.test" {
		t.Fatal("before the confirmation the email stays and the pending address shows")
	}
	if rec := login(t, tc, "new@example.test", mailTestPassword); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the new address works before the confirmation: %d", rec.Code)
	}

	if rec := confirmChange(t, tc, "bogus"); rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus token: %d", rec.Code)
	}
	if rec := confirmChange(t, tc, " "+token+" "); rec.Code != http.StatusOK {
		t.Fatalf("confirm: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := confirmChange(t, tc, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("reused token: %d", rec.Code)
	}

	notice := only(t, tc, acc.email)
	if !strings.Contains(notice.Text, "n***@example.test") || strings.Contains(notice.Text, "new@example.test") {
		t.Fatalf("notice = %q", notice.Text)
	}
	if rec := login(t, tc, acc.email, mailTestPassword); rec.Code != http.StatusUnauthorized {
		t.Fatalf("login with the old address: %d", rec.Code)
	}
	newAccess := accessToken(t, login(t, tc, "new@example.test", mailTestPassword))
	if profileField(t, tc, newAccess, "email") != "new@example.test" || profileField(t, tc, newAccess, "email_verified_at") == nil {
		t.Fatal("the profile does not show the confirmed new address")
	}
	if profileField(t, tc, newAccess, "pending_email") != nil {
		t.Fatal("pending_email survived the confirmation")
	}
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/profile", acc.access, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the old access token still works: %d", rec.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.Header.Set("Cookie", refresh)
	out := httptest.NewRecorder()
	tc.Router.ServeHTTP(out, req)
	if out.Code != http.StatusUnauthorized {
		t.Fatalf("the old refresh token still works: %d", out.Code)
	}
	if rec := postAuth(t, tc, "reset-password", map[string]any{"token": resetToken, "new_password": "attacker-chosen-pass"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a reset link from before the change still works: %d", rec.Code)
	}
}

func TestEmailChange_Rejections(t *testing.T) {
	tc := newTestContext(t)
	acc := newChangeAccount(t, tc, "ann@example.test")
	signUp(t, tc, "bob@example.test")
	tc.Mail.Reset()

	if rec := requestChange(t, tc, acc.access, "new@example.test", "wrong-password"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: %d", rec.Code)
	}
	if rec := requestChange(t, tc, acc.access, "ANN@example.test", mailTestPassword); rec.Code != http.StatusBadRequest {
		t.Fatalf("same address: %d", rec.Code)
	}
	if rec := requestChange(t, tc, acc.access, "Bob@example.test", mailTestPassword); rec.Code != http.StatusConflict {
		t.Fatalf("taken address: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, changePath, "", map[string]any{"new_email": "x@example.test", "password": "x"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", rec.Code)
	}
	if rec := confirmChange(t, tc, ""); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("empty token: %d", rec.Code)
	}
	if len(tc.Mail.Messages()) != 0 {
		t.Fatalf("rejected requests sent mail: %+v", tc.Mail.Messages())
	}
	var rows int64
	tc.DB.Table("email_tokens").Where("kind = 'change_email'").Count(&rows)
	if rows != 0 {
		t.Fatalf("%d change tokens stored by rejected requests", rows)
	}
}

func TestEmailChange_AddressTakenBetweenRequestAndConfirmation(t *testing.T) {
	tc := newTestContext(t)
	acc := newChangeAccount(t, tc, "ann@example.test")
	token := startChange(t, tc, acc, "new@example.test")
	signUp(t, tc, "new@example.test")
	tc.Mail.Reset()

	if rec := confirmChange(t, tc, token); rec.Code != http.StatusConflict {
		t.Fatalf("confirm: %d: %s", rec.Code, rec.Body.String())
	}
	if len(tc.Mail.Messages()) != 0 {
		t.Fatal("a notice was sent although nothing changed")
	}
	if rec := login(t, tc, acc.email, mailTestPassword); rec.Code != http.StatusOK {
		t.Fatalf("the account lost its address: %d", rec.Code)
	}
	if profileField(t, tc, acc.access, "email") != acc.email {
		t.Fatal("the failed confirmation changed the address or revoked the session")
	}
}

func TestEmailChange_ExpiredReplacedAndCooldown(t *testing.T) {
	tc := newTestContext(t)
	acc := newChangeAccount(t, tc, "ann@example.test")
	expired := startChange(t, tc, acc, "one@example.test")

	if rec := requestChange(t, tc, acc.access, "two@example.test", mailTestPassword); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("cooldown: %d: %s", rec.Code, rec.Body.String())
	}
	if len(tc.Mail.To("two@example.test")) != 0 {
		t.Fatal("a mail was sent inside the cool-down")
	}

	tc.DB.Exec("UPDATE email_tokens SET expires_at = now() - interval '1 second' WHERE kind = 'change_email'")
	if rec := confirmChange(t, tc, expired); rec.Code != http.StatusBadRequest {
		t.Fatalf("expired: %d", rec.Code)
	}
	if profileField(t, tc, acc.access, "pending_email") != nil {
		t.Fatal("an expired change still shows as pending")
	}

	tc.DB.Exec("UPDATE email_tokens SET created_at = now() - interval '5 minutes'")
	replaced := startChange(t, tc, acc, "two@example.test")
	var rows int64
	tc.DB.Table("email_tokens").Where("kind = 'change_email'").Count(&rows)
	if rows != 1 {
		t.Fatalf("pending change tokens = %d, want 1", rows)
	}
	if rec := confirmChange(t, tc, expired); rec.Code != http.StatusBadRequest {
		t.Fatalf("replaced: %d", rec.Code)
	}
	if rec := confirmChange(t, tc, replaced); rec.Code != http.StatusOK {
		t.Fatalf("confirm the replacement: %d", rec.Code)
	}
	if rec := login(t, tc, "two@example.test", mailTestPassword); rec.Code != http.StatusOK {
		t.Fatalf("login with the new address: %d", rec.Code)
	}
}

func TestEmailChange_CancelAndDeliveryFailure(t *testing.T) {
	tc := newTestContext(t)
	acc := newChangeAccount(t, tc, "ann@example.test")

	tc.Mail.FailWith(http.ErrHandlerTimeout)
	if rec := requestChange(t, tc, acc.access, "new@example.test", mailTestPassword); rec.Code != http.StatusAccepted {
		t.Fatalf("a queued mail must not fail the request: %d", rec.Code)
	}
	if len(tc.Mail.To("new@example.test")) != 0 {
		t.Fatal("the mail went out although the transport was down")
	}
	tc.Mail.FailWith(nil)
	if err := tc.DB.Exec("UPDATE outbox_events SET next_attempt_at = now() WHERE event_type = 'delivery.mail' AND published_at IS NULL").Error; err != nil {
		t.Fatal(err)
	}
	if err := service.DrainOutbox(context.Background(), tc.Workers...); err != nil {
		t.Fatal(err)
	}
	if len(tc.Mail.To("new@example.test")) != 1 {
		t.Fatal("the queued confirmation was not delivered once the transport recovered")
	}
	token := linkToken(t, only(t, tc, "new@example.test"), "/confirm-email-change")
	if profileField(t, tc, acc.access, "pending_email") != "new@example.test" {
		t.Fatal("pending_email missing")
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/email-change", acc.access, nil); rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d", rec.Code)
	}
	if profileField(t, tc, acc.access, "pending_email") != nil {
		t.Fatal("pending_email survived the cancellation")
	}
	if rec := confirmChange(t, tc, token); rec.Code != http.StatusBadRequest {
		t.Fatalf("a canceled link works: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/email-change", acc.access, nil); rec.Code != http.StatusOK {
		t.Fatalf("cancel without a pending change: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile/email-change", "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous cancel: %d", rec.Code)
	}
}

func TestEmailChange_PendingAddressIsPrivateAndRemovedWithTheAccount(t *testing.T) {
	tc := newTestContext(t)
	ann := newChangeAccount(t, tc, "ann@example.test")
	bob := newChangeAccount(t, tc, "bob@example.test")
	startChange(t, tc, ann, "new@example.test")

	if profileField(t, tc, bob.access, "pending_email") != nil {
		t.Fatal("another user sees a pending address")
	}
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/users/search?q=ann", bob.access, nil)
	if strings.Contains(rec.Body.String(), "new@example.test") {
		t.Fatalf("the pending address leaked through the search: %s", rec.Body.String())
	}

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile", ann.access, map[string]any{"password": mailTestPassword}); rec.Code != http.StatusOK {
		t.Fatalf("delete account: %d", rec.Code)
	}
	var rows int64
	tc.DB.Table("email_tokens").Where("kind = 'change_email'").Count(&rows)
	if rows != 0 {
		t.Fatalf("%d change tokens survived the deletion of the account", rows)
	}
}

func TestEmailChange_ValidationErrors(t *testing.T) {
	tc := newTestContext(t)
	acc := newChangeAccount(t, tc, "ann@example.test")
	for _, body := range []map[string]any{{}, {"new_email": "nope", "password": mailTestPassword}, {"new_email": "x@example.test"}, {"password": mailTestPassword}} {
		if rec := request(t, tc.Router, http.MethodPost, changePath, acc.access, body); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %v: %d", body, rec.Code)
		}
	}
	if len(tc.Mail.Messages()) != 0 {
		t.Fatal("invalid requests sent mail")
	}
}
