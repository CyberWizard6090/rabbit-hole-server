package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"rabbit-hole-server/internal/app"
	"rabbit-hole-server/internal/config"
	"rabbit-hole-server/internal/safehttp"
	"rabbit-hole-server/internal/service"
)

type hookRequest struct {
	header http.Header
	body   []byte
}

type hookReceiver struct {
	server *httptest.Server
	mu     sync.Mutex
	status int
	got    []hookRequest
}

func newHookReceiver(t *testing.T) *hookReceiver {
	t.Helper()
	r := &hookReceiver{status: http.StatusOK}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.got = append(r.got, hookRequest{header: req.Header.Clone(), body: body})
		status := r.status
		r.mu.Unlock()
		w.WriteHeader(status)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *hookReceiver) setStatus(status int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.status = status
}

func (r *hookReceiver) requests() []hookRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]hookRequest(nil), r.got...)
}

func (r *hookReceiver) last(t *testing.T) hookRequest {
	t.Helper()
	all := r.requests()
	if len(all) == 0 {
		t.Fatal("the receiver got nothing")
	}
	return all[len(all)-1]
}

func (r *hookReceiver) body(t *testing.T, req hookRequest) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(req.body, &out); err != nil {
		t.Fatalf("receiver body: %v: %s", err, req.body)
	}
	return out
}

func signatureValid(req hookRequest, secret string) bool {
	return req.header.Get(service.WebhookSignatureHeader) ==
		service.SignWebhook(secret, req.header.Get(service.WebhookTimestampHeader), req.body)
}

func webhookContext(t *testing.T, maxFailures int) *TestContext {
	t.Helper()
	client := safehttp.New(safehttp.AllowLoopbackForTests(), safehttp.WithMaxRedirects(0), safehttp.WithTimeout(3*time.Second))
	return newTestContextWith(t, func(cfg *config.Config) {
		cfg.Environment = "development"
		cfg.Notifications.WebhookMaxFailures = maxFailures
	}, app.WithWebhookClient(client))
}

func addWebhook(t *testing.T, tc *TestContext, token, destination string) (uint, string) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodPost, channelsURL(""), token, map[string]any{"kind": "webhook", "destination": destination})
	if rec.Code != http.StatusCreated {
		t.Fatalf("add webhook: %d: %s", rec.Code, rec.Body.String())
	}
	data := dataObject(t, rec)
	secret, _ := data["secret"].(string)
	return responseID(t, rec), secret
}

func confirmWebhook(t *testing.T, tc *TestContext, rcv *hookReceiver) {
	t.Helper()
	token, _ := rcv.body(t, rcv.last(t))["confirmation_token"].(string)
	if token == "" {
		t.Fatalf("confirmation request has no token: %s", rcv.last(t).body)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/confirm"), "", map[string]any{"token": token}); rec.Code != http.StatusOK {
		t.Fatalf("confirm webhook: %d: %s", rec.Code, rec.Body.String())
	}
}

func webhookChannelItem(t *testing.T, tc *TestContext, token string, id uint) map[string]any {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, channelsURL(""), token, nil)
	for _, raw := range decodeJSON(t, rec)["data"].([]any) {
		item := raw.(map[string]any)
		if got, ok := item["id"].(float64); ok && uint(got) == id {
			return item
		}
	}
	t.Fatalf("channel %d not listed: %s", id, rec.Body.String())
	return nil
}

func webhookUsable(t *testing.T, tc *TestContext, token, typ string) bool {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/notifications/preferences", token, nil)
	for _, raw := range decodeJSON(t, rec)["data"].([]any) {
		cell := raw.(map[string]any)
		if cell["channel"] == "webhook" && cell["type"] == typ {
			return cell["usable"] == true
		}
	}
	t.Fatal("no webhook cell in the matrix")
	return false
}

func TestNotificationWebhook_ConfirmationDeliverySignaturePreferencesAndSecretLifecycle(t *testing.T) {
	tc := webhookContext(t, 10)
	f := newNotifyFixture(t, tc)
	rcv := newHookReceiver(t)

	channelID, secret := addWebhook(t, tc, f.tokenA, rcv.server.URL+"/hook")
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("the secret is shown on creation: %q", secret)
	}
	if got := rcv.requests(); len(got) != 1 {
		t.Fatalf("exactly one confirmation request expected, got %d", len(got))
	}
	confirmation := rcv.last(t)
	if confirmation.header.Get(service.WebhookEventHeader) != service.WebhookConfirmationEvent || !signatureValid(confirmation, secret) {
		t.Fatalf("confirmation headers = %v", confirmation.header)
	}
	if webhookUsable(t, tc, f.tokenA, "mention") {
		t.Fatal("an unconfirmed webhook must not be usable")
	}
	item := webhookChannelItem(t, tc, f.tokenA, channelID)
	if item["verified"] != false || item["secret"] != nil || item["destination"] != rcv.server.URL+"/hook" {
		t.Fatalf("listed channel = %v", item)
	}

	f.comment(t, f.owner.token, f.taskID, "hello @a", f.memberA)
	if got := rcv.requests(); len(got) != 1 {
		t.Fatalf("an unconfirmed webhook received an event: %d requests", len(got))
	}

	var stored string
	tc.DB.Raw("SELECT token_hash FROM channel_confirmations WHERE channel_id = ?", channelID).Scan(&stored)
	token, _ := rcv.body(t, confirmation)["confirmation_token"].(string)
	if stored == "" || strings.Contains(stored, token) {
		t.Fatalf("confirmation token must be stored hashed: %q", stored)
	}
	confirmWebhook(t, tc, rcv)
	if !webhookUsable(t, tc, f.tokenA, "mention") {
		t.Fatal("a confirmed webhook is usable")
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/confirm"), "", map[string]any{"token": token}); rec.Code != http.StatusBadRequest {
		t.Fatalf("token reuse: %d", rec.Code)
	}

	f.comment(t, f.owner.token, f.taskID, "hello again", f.memberA)
	if got := rcv.requests(); len(got) != 2 {
		t.Fatalf("a confirmed webhook gets the mention: %d requests", len(got))
	}
	delivery := rcv.last(t)
	payload := rcv.body(t, delivery)
	if !signatureValid(delivery, secret) || delivery.header.Get(service.WebhookEventHeader) != "mention" ||
		delivery.header.Get(service.WebhookDeliveryHeader) == "" || delivery.header.Get(service.WebhookTimestampHeader) == "" {
		t.Fatalf("delivery headers = %v", delivery.header)
	}
	task := payload["task"].(map[string]any)
	if payload["type"] != "mention" || uint(task["id"].(float64)) != f.taskID || task["title"] != "shared task" ||
		payload["delivery_id"] != delivery.header.Get(service.WebhookDeliveryHeader) || payload["created_at"] == nil ||
		!strings.HasSuffix(payload["link"].(string), "/tasks/"+itoa(f.taskID)) || payload["detail"] != "hello again" {
		t.Fatalf("payload = %v", payload)
	}
	if strings.Contains(string(delivery.body), "@example.test") {
		t.Fatalf("payload leaks an e-mail address: %s", delivery.body)
	}

	f.watch(t, f.tokenA, http.MethodPut)
	f.comment(t, f.owner.token, f.taskID, "plain comment")
	if got := rcv.requests(); len(got) != 2 {
		t.Fatalf("comments are off for the webhook by default: %d", len(got))
	}
	setPreference(t, tc, f.tokenA, "comment", "webhook", true)
	f.comment(t, f.owner.token, f.taskID, "plain comment 2")
	if got := rcv.requests(); len(got) != 3 || rcv.body(t, rcv.last(t))["type"] != "comment" {
		t.Fatalf("comment preference: %d requests", len(got))
	}
	setPreference(t, tc, f.tokenA, "mention", "webhook", false)
	f.comment(t, f.owner.token, f.taskID, "mention off", f.memberA)
	if got := rcv.requests(); len(got) != 3 {
		t.Fatalf("a switched-off type must not be sent: %d requests", len(got))
	}
	setPreference(t, tc, f.tokenA, "mention", "webhook", true)

	var enc string
	tc.DB.Raw("SELECT secret_enc FROM notification_channels WHERE id = ?", channelID).Scan(&enc)
	if enc == "" || strings.Contains(enc, secret) || strings.Contains(enc, strings.TrimPrefix(secret, "whsec_")) {
		t.Fatalf("the secret must be stored encrypted: %q", enc)
	}
	if webhookChannelItem(t, tc, f.tokenA, channelID)["secret"] != nil {
		t.Fatal("the secret is shown only once")
	}

	rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/rotate-secret"), f.tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d: %s", rec.Code, rec.Body.String())
	}
	rotated, _ := dataObject(t, rec)["secret"].(string)
	if !strings.HasPrefix(rotated, "whsec_") || rotated == secret {
		t.Fatalf("rotated secret = %q", rotated)
	}
	f.comment(t, f.owner.token, f.taskID, "after rotation", f.memberA)
	after := rcv.last(t)
	if !signatureValid(after, rotated) || signatureValid(after, secret) {
		t.Fatal("deliveries must be signed with the new secret only")
	}
	if webhookChannelItem(t, tc, f.tokenA, channelID)["secret"] != nil {
		t.Fatal("rotation response is the only place the new secret appears")
	}

	count := len(rcv.requests())
	if rec := request(t, tc.Router, http.MethodDelete, channelsURL("/"+itoa(channelID)), f.tokenA, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	f.comment(t, f.owner.token, f.taskID, "after delete", f.memberA)
	if len(rcv.requests()) != count {
		t.Fatal("a deleted webhook keeps receiving events")
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/rotate-secret"), f.tokenA, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("rotate after delete: %d", rec.Code)
	}
}

func TestNotificationWebhook_OtherUsersCannotTouchTheChannel(t *testing.T) {
	tc := webhookContext(t, 10)
	f := newNotifyFixture(t, tc)
	rcv := newHookReceiver(t)
	channelID, _ := addWebhook(t, tc, f.tokenA, rcv.server.URL)

	for _, token := range []string{f.tokenB, f.tokenOutsider} {
		path := channelsURL("/" + itoa(channelID))
		if rec := request(t, tc.Router, http.MethodPost, path+"/rotate-secret", token, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("foreign rotate: %d", rec.Code)
		}
		if rec := request(t, tc.Router, http.MethodPost, path+"/resend", token, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("foreign resend: %d", rec.Code)
		}
		if rec := request(t, tc.Router, http.MethodDelete, path, token, nil); rec.Code != http.StatusNotFound {
			t.Fatalf("foreign delete: %d", rec.Code)
		}
		rec := request(t, tc.Router, http.MethodGet, channelsURL(""), token, nil)
		if strings.Contains(rec.Body.String(), rcv.server.URL) {
			t.Fatalf("foreign listing leaks the destination: %s", rec.Body.String())
		}
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/rotate-secret"), "", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous rotate: %d", rec.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/rotate-secret"), f.tokenA, nil); rec.Code != http.StatusOK {
		t.Fatalf("owner rotate: %d", rec.Code)
	}

	extra := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "email", "destination": "extra-hook@example.test"})
	if extra.Code != http.StatusCreated {
		t.Fatalf("add email: %d", extra.Code)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(responseID(t, extra))+"/rotate-secret"), f.tokenA, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("an e-mail channel has no secret: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNotificationWebhook_DisabledAfterConsecutiveFailuresAndRotationReenables(t *testing.T) {
	tc := webhookContext(t, 2)
	f := newNotifyFixture(t, tc)
	rcv := newHookReceiver(t)
	channelID, secret := addWebhook(t, tc, f.tokenA, rcv.server.URL)
	confirmWebhook(t, tc, rcv)

	rcv.setStatus(http.StatusBadRequest)
	f.comment(t, f.owner.token, f.taskID, "one", f.memberA)
	item := webhookChannelItem(t, tc, f.tokenA, channelID)
	if item["disabled_at"] != nil || item["last_error"] != "the endpoint answered HTTP 400" {
		t.Fatalf("after one failure: %v", item)
	}
	rcv.setStatus(http.StatusOK)
	f.comment(t, f.owner.token, f.taskID, "recovered", f.memberA)
	if item := webhookChannelItem(t, tc, f.tokenA, channelID); item["last_error"] != nil {
		t.Fatalf("a success resets the failure state: %v", item)
	}

	rcv.setStatus(http.StatusBadRequest)
	f.comment(t, f.owner.token, f.taskID, "two", f.memberA)
	f.comment(t, f.owner.token, f.taskID, "three", f.memberA)
	item = webhookChannelItem(t, tc, f.tokenA, channelID)
	if item["disabled_at"] == nil || item["last_error"] != "the endpoint answered HTTP 400" {
		t.Fatalf("channel must be disabled after two consecutive failures: %v", item)
	}
	if strings.Contains(item["last_error"].(string), secret) || strings.Contains(item["last_error"].(string), rcv.server.URL) {
		t.Fatalf("last_error leaks: %v", item["last_error"])
	}
	if webhookUsable(t, tc, f.tokenA, "mention") {
		t.Fatal("a disabled webhook is not usable")
	}

	seen := len(rcv.requests())
	f.comment(t, f.owner.token, f.taskID, "four", f.memberA)
	if len(rcv.requests()) != seen {
		t.Fatal("a disabled webhook keeps receiving events")
	}

	rcv.setStatus(http.StatusOK)
	rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/rotate-secret"), f.tokenA, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate: %d", rec.Code)
	}
	if item := webhookChannelItem(t, tc, f.tokenA, channelID); item["disabled_at"] != nil || item["last_error"] != nil {
		t.Fatalf("rotation re-enables the channel: %v", item)
	}
	f.comment(t, f.owner.token, f.taskID, "five", f.memberA)
	if len(rcv.requests()) != seen+1 {
		t.Fatal("a re-enabled webhook receives events again")
	}
}

func TestNotificationWebhook_RefusesPrivateAndInsecureDestinations(t *testing.T) {
	client := safehttp.New(safehttp.WithMaxRedirects(0), safehttp.WithTimeout(time.Second))
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Environment = "test" }, app.WithWebhookClient(client))
	f := newNotifyFixture(t, tc)

	for _, destination := range []string{
		"https://127.0.0.1/hook", "https://10.0.0.5/hook", "https://169.254.169.254/latest", "https://192.168.1.1/x",
		"https://[::1]/hook", "https://localhost/hook", "https://metadata.internal/x", "http://example.test/hook",
		"https://user:pass@example.test/hook", "ftp://example.test/x", "not a url", "https://example.test:8443/x",
	} {
		rec := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "webhook", "destination": destination})
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: %d: %s", destination, rec.Code, rec.Body.String())
		}
	}
	var count int64
	tc.DB.Raw("SELECT count(*) FROM notification_channels").Scan(&count)
	if count != 0 {
		t.Fatalf("refused destinations must leave no channel behind: %d", count)
	}
}

func TestNotificationWebhook_UnreachableEndpointLeavesNoChannelAndDuplicatesAreRejected(t *testing.T) {
	tc := webhookContext(t, 10)
	f := newNotifyFixture(t, tc)
	rcv := newHookReceiver(t)

	rcv.setStatus(http.StatusBadRequest)
	rec := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "webhook", "destination": rcv.server.URL})
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("a rejected confirmation: %d: %s", rec.Code, rec.Body.String())
	}
	var count int64
	tc.DB.Raw("SELECT count(*) FROM notification_channels WHERE kind = 'webhook'").Scan(&count)
	if count != 0 {
		t.Fatalf("a webhook that cannot be reached must not be kept: %d", count)
	}

	rcv.setStatus(http.StatusOK)
	addWebhook(t, tc, f.tokenA, rcv.server.URL)
	dup := request(t, tc.Router, http.MethodPost, channelsURL(""), f.tokenA, map[string]any{"kind": "webhook", "destination": rcv.server.URL})
	if dup.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d: %s", dup.Code, dup.Body.String())
	}
}

func TestNotificationWebhook_ResendSendsANewSignedConfirmationAndOldTokenStopsWorking(t *testing.T) {
	tc := webhookContext(t, 10)
	f := newNotifyFixture(t, tc)
	rcv := newHookReceiver(t)
	channelID, secret := addWebhook(t, tc, f.tokenA, rcv.server.URL)
	oldToken, _ := rcv.body(t, rcv.last(t))["confirmation_token"].(string)

	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/resend"), f.tokenA, nil); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("resend cooldown: %d", rec.Code)
	}
	if err := tc.DB.Exec("UPDATE channel_confirmations SET created_at = now() - interval '5 minutes' WHERE channel_id = ?", channelID).Error; err != nil {
		t.Fatal(err)
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/resend"), f.tokenA, nil); rec.Code != http.StatusAccepted {
		t.Fatalf("resend: %d: %s", rec.Code, rec.Body.String())
	}
	if got := rcv.requests(); len(got) != 2 || !signatureValid(rcv.last(t), secret) {
		t.Fatalf("resend must send a second signed confirmation: %d", len(got))
	}
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/confirm"), "", map[string]any{"token": oldToken}); rec.Code != http.StatusBadRequest {
		t.Fatalf("a replaced token must stop working: %d", rec.Code)
	}
	confirmWebhook(t, tc, rcv)
	if rec := request(t, tc.Router, http.MethodPost, channelsURL("/"+itoa(channelID)+"/resend"), f.tokenA, nil); rec.Code != http.StatusConflict {
		t.Fatalf("resend of a confirmed channel: %d", rec.Code)
	}
}
