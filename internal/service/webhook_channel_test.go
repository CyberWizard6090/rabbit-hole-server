package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/safehttp"
)

type recordedDelivery struct {
	channelID uint
	failure   string
	max       int
}

type fakeWebhookRepo struct {
	mu         sync.Mutex
	target     *domain.WebhookTarget
	secrets    map[uint]string
	deliveries []recordedDelivery
}

func (f *fakeWebhookRepo) SetSecret(_ context.Context, _, channelID uint, enc string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.secrets == nil {
		f.secrets = map[uint]string{}
	}
	f.secrets[channelID] = enc
	return nil
}

func (f *fakeWebhookRepo) ActiveTarget(context.Context, uint, string) (*domain.WebhookTarget, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.target == nil {
		return nil, domain.ErrNotFound
	}
	return f.target, nil
}

func (f *fakeWebhookRepo) RecordDelivery(_ context.Context, id uint, failure string, max int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deliveries = append(f.deliveries, recordedDelivery{channelID: id, failure: failure, max: max})
	return nil
}

func (f *fakeWebhookRepo) recorded() []recordedDelivery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]recordedDelivery(nil), f.deliveries...)
}

type webhookHarness struct {
	channel *webhookChannel
	repo    *fakeWebhookRepo
	box     *SecretBox
	secret  string
}

func newWebhookHarness(t *testing.T, client WebhookClient, allowHTTP bool) *webhookHarness {
	t.Helper()
	box, err := NewSecretBox("0123456789012345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	secret := "whsec_test-secret"
	sealed, err := box.Seal(7, secret)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeWebhookRepo{target: &domain.WebhookTarget{ChannelID: 7, SecretEnc: sealed}}
	ch := NewWebhookChannel(WebhookConfig{
		Client: client, Repo: repo, Channels: newFakeChannelRepo(), Box: box,
		FrontendURL: "https://app.example.test/", AllowHTTP: allowHTTP, MaxFailures: 4,
	}).(*webhookChannel)
	ch.backoff = time.Millisecond
	t.Cleanup(func() { _ = ch.Shutdown(context.Background()) })
	return &webhookHarness{channel: ch, repo: repo, box: box, secret: secret}
}

func loopbackClient() *safehttp.Client {
	return safehttp.New(safehttp.AllowLoopbackForTests(), safehttp.WithMaxRedirects(0), safehttp.WithTimeout(2*time.Second))
}

type capturedRequest struct {
	header http.Header
	body   []byte
}

type receiver struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []capturedRequest
	hits     atomic.Int32
}

func newReceiver(t *testing.T, handler func(hit int, w http.ResponseWriter)) *receiver {
	t.Helper()
	r := &receiver{}
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		body, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.requests = append(r.requests, capturedRequest{header: req.Header.Clone(), body: body})
		r.mu.Unlock()
		handler(int(r.hits.Add(1)), w)
	}))
	t.Cleanup(r.server.Close)
	return r
}

func (r *receiver) captured() []capturedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]capturedRequest(nil), r.requests...)
}

func sampleDelivery(destination string) Delivery {
	return Delivery{
		RecipientID: 3, Locale: "en", Destination: destination,
		Content: NotificationContent{
			Type:   mustType(domain.NotificationMention),
			TaskID: 42, TaskTitle: "Ship\nit", WorkspaceID: 5,
			Actor:   domain.CommentUser{ID: 9, FirstName: "Ada", LastName: "Lovelace", Username: "ada"},
			ActorID: 9, Payload: map[string]any{"comment_excerpt": "hello there"},
		},
	}
}

func mustType(key string) domain.NotificationType {
	typ, ok := domain.NotificationTypeByKey(key)
	if !ok {
		panic("unknown type " + key)
	}
	return typ
}

func TestSignWebhookMatchesIndependentHMAC(t *testing.T) {
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte("1700000000.{\"a\":1}"))
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := SignWebhook("s3cret", "1700000000", []byte(`{"a":1}`)); got != want {
		t.Fatalf("signature = %s, want %s", got, want)
	}
	if SignWebhook("other", "1700000000", []byte(`{"a":1}`)) == want {
		t.Fatal("a different secret must change the signature")
	}
	if SignWebhook("s3cret", "1700000001", []byte(`{"a":1}`)) == want {
		t.Fatal("a different timestamp must change the signature")
	}
}

func TestWebhookDeliverySignsAndDescribesTheEvent(t *testing.T) {
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) })
	h := newWebhookHarness(t, loopbackClient(), true)
	if err := h.channel.Deliver(context.Background(), sampleDelivery(rcv.server.URL+"/hook")); err != nil {
		t.Fatal(err)
	}
	h.channel.Wait()

	reqs := rcv.captured()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	req := reqs[0]
	timestamp := req.header.Get(WebhookTimestampHeader)
	if got, want := req.header.Get(WebhookSignatureHeader), SignWebhook(h.secret, timestamp, req.body); got != want {
		t.Fatalf("signature header = %q, want %q", got, want)
	}
	if req.header.Get(WebhookEventHeader) != "mention" || req.header.Get("Content-Type") != "application/json" {
		t.Fatalf("headers = %v", req.header)
	}
	deliveryID := req.header.Get(WebhookDeliveryHeader)
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(deliveryID) {
		t.Fatalf("delivery id = %q", deliveryID)
	}
	if strings.Contains(string(req.body), "private.test") || strings.Contains(string(req.body), "@") {
		t.Fatalf("payload leaks an e-mail address: %s", req.body)
	}
	var payload map[string]any
	if err := json.Unmarshal(req.body, &payload); err != nil {
		t.Fatal(err)
	}
	task := payload["task"].(map[string]any)
	actor := payload["actor"].(map[string]any)
	if payload["type"] != "mention" || payload["delivery_id"] != deliveryID || task["id"].(float64) != 42 ||
		task["title"] != "Ship it" || actor["display_name"] != "Ada Lovelace" ||
		payload["link"] != "https://app.example.test/tasks/42" || payload["detail"] != "hello there" ||
		payload["created_at"] == nil || payload["summary"] == "" {
		t.Fatalf("payload = %v", payload)
	}
	if rec := h.repo.recorded(); len(rec) != 1 || rec[0].failure != "" || rec[0].channelID != 7 || rec[0].max != 4 {
		t.Fatalf("recorded = %+v", rec)
	}
}

func TestWebhookRetriesServerErrorsWithTheSameDeliveryID(t *testing.T) {
	rcv := newReceiver(t, func(hit int, w http.ResponseWriter) {
		if hit < 3 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	h := newWebhookHarness(t, loopbackClient(), true)
	_ = h.channel.Deliver(context.Background(), sampleDelivery(rcv.server.URL))
	h.channel.Wait()

	reqs := rcv.captured()
	if len(reqs) != 3 {
		t.Fatalf("attempts = %d, want 3", len(reqs))
	}
	for _, req := range reqs {
		if req.header.Get(WebhookDeliveryHeader) != reqs[0].header.Get(WebhookDeliveryHeader) {
			t.Fatal("a retry must keep the delivery id")
		}
		if req.header.Get(WebhookSignatureHeader) != SignWebhook(h.secret, req.header.Get(WebhookTimestampHeader), req.body) {
			t.Fatal("every attempt must carry a valid signature")
		}
	}
	if rec := h.repo.recorded(); len(rec) != 1 || rec[0].failure != "" {
		t.Fatalf("a delivery that succeeds after retries is a success: %+v", rec)
	}
}

func TestWebhookGivesUpAfterBoundedRetriesAndSanitizesTheError(t *testing.T) {
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("secret internal stack trace"))
	})
	h := newWebhookHarness(t, loopbackClient(), true)
	_ = h.channel.Deliver(context.Background(), sampleDelivery(rcv.server.URL))
	h.channel.Wait()

	if got := rcv.hits.Load(); got != webhookMaxAttempts {
		t.Fatalf("attempts = %d, want %d", got, webhookMaxAttempts)
	}
	rec := h.repo.recorded()
	if len(rec) != 1 || rec[0].failure != "the endpoint answered HTTP 500" {
		t.Fatalf("recorded = %+v", rec)
	}
	if strings.Contains(rec[0].failure, "stack trace") || strings.Contains(rec[0].failure, rcv.server.URL) {
		t.Fatalf("failure text leaks: %q", rec[0].failure)
	}
}

func TestWebhookDoesNotRetryClientErrors(t *testing.T) {
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusGone) })
	h := newWebhookHarness(t, loopbackClient(), true)
	_ = h.channel.Deliver(context.Background(), sampleDelivery(rcv.server.URL))
	h.channel.Wait()
	if rcv.hits.Load() != 1 {
		t.Fatalf("attempts = %d, want 1", rcv.hits.Load())
	}
	if rec := h.repo.recorded(); len(rec) != 1 || rec[0].failure != "the endpoint answered HTTP 410" {
		t.Fatalf("recorded = %+v", rec)
	}
}

func TestWebhookTimeoutIsRetriedAndReported(t *testing.T) {
	var hits atomic.Int32
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(3 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	client := safehttp.New(safehttp.AllowLoopbackForTests(), safehttp.WithMaxRedirects(0), safehttp.WithTimeout(100*time.Millisecond))
	h := newWebhookHarness(t, client, true)
	_ = h.channel.Deliver(context.Background(), sampleDelivery(slow.URL))
	h.channel.Wait()
	if hits.Load() != webhookMaxAttempts {
		t.Fatalf("attempts = %d", hits.Load())
	}
	if rec := h.repo.recorded(); len(rec) != 1 || rec[0].failure != "the request timed out" {
		t.Fatalf("recorded = %+v", rec)
	}
}

func TestWebhookNeverFollowsRedirects(t *testing.T) {
	var targetHits atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusFound)
	})
	h := newWebhookHarness(t, loopbackClient(), true)
	_ = h.channel.Deliver(context.Background(), sampleDelivery(rcv.server.URL))
	h.channel.Wait()
	if targetHits.Load() != 0 || rcv.hits.Load() != 1 {
		t.Fatalf("redirect was followed or retried: target=%d source=%d", targetHits.Load(), rcv.hits.Load())
	}
	if rec := h.repo.recorded(); len(rec) != 1 || !strings.Contains(rec[0].failure, "redirect") {
		t.Fatalf("recorded = %+v", rec)
	}
}

func TestWebhookRefusesPrivateTargets(t *testing.T) {
	for _, target := range []string{
		"https://127.0.0.1/hook", "https://10.1.2.3/hook", "https://169.254.169.254/latest/meta-data",
		"https://192.168.0.10/hook", "https://[::1]/hook",
	} {
		h := newWebhookHarness(t, safehttp.New(safehttp.WithMaxRedirects(0)), false)
		_ = h.channel.Deliver(context.Background(), sampleDelivery(target))
		h.channel.Wait()
		rec := h.repo.recorded()
		if len(rec) != 1 || rec[0].failure != "the destination address is not allowed" {
			t.Fatalf("%s: recorded = %+v", target, rec)
		}
		err := webhookConfirmation{h.channel}.ChallengeChannel(context.Background(),
			domain.NotificationChannel{ID: 7, Destination: target, SecretEnc: h.repo.target.SecretEnc}, "tok", "en", time.Hour)
		if !errors.Is(err, domain.ErrInvalidDestination) {
			t.Fatalf("%s: confirmation error = %v", target, err)
		}
	}
}

func TestWebhookValidateDestination(t *testing.T) {
	prod := newWebhookHarness(t, safehttp.New(), false)
	dev := newWebhookHarness(t, safehttp.New(), true)
	valid := map[string]string{
		" https://Hooks.Example.test/in?x=1#frag ": "https://hooks.example.test/in?x=1",
		"https://hooks.example.test:443/a":         "https://hooks.example.test:443/a",
	}
	for in, want := range valid {
		got, err := prod.channel.ValidateDestination(in)
		if err != nil || got != want {
			t.Fatalf("%q -> %q, %v", in, got, err)
		}
	}
	invalid := []string{
		"", "hooks.example.test", "ftp://hooks.example.test", "http://hooks.example.test/in", "https://user:pw@hooks.example.test/",
		"https://localhost/hook", "https://service.internal/hook", "https://hooks.example.test:8443/x", "https://exa mple.test/",
		"https://hooks.example.test/" + strings.Repeat("a", 2100),
	}
	for _, in := range invalid {
		if _, err := prod.channel.ValidateDestination(in); !errors.Is(err, domain.ErrInvalidDestination) {
			t.Fatalf("%q must be invalid, got %v", in, err)
		}
	}
	if got, err := dev.channel.ValidateDestination("http://hooks.example.test/in"); err != nil || got != "http://hooks.example.test/in" {
		t.Fatalf("http in development: %q, %v", got, err)
	}
}

func TestWebhookConfirmationBodyCarriesTheTokenAndIsSigned(t *testing.T) {
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusOK) })
	h := newWebhookHarness(t, loopbackClient(), true)
	err := webhookConfirmation{h.channel}.ChallengeChannel(context.Background(),
		domain.NotificationChannel{ID: 7, Destination: rcv.server.URL, SecretEnc: h.repo.target.SecretEnc}, "tok en", "en", 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	reqs := rcv.captured()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	req := reqs[0]
	if req.header.Get(WebhookEventHeader) != WebhookConfirmationEvent ||
		req.header.Get(WebhookSignatureHeader) != SignWebhook(h.secret, req.header.Get(WebhookTimestampHeader), req.body) {
		t.Fatalf("headers = %v", req.header)
	}
	var body map[string]any
	if err := json.Unmarshal(req.body, &body); err != nil {
		t.Fatal(err)
	}
	if body["confirmation_token"] != "tok en" || body["type"] != WebhookConfirmationEvent ||
		body["confirm_url"] != "https://app.example.test/confirm-channel?token=tok+en" || body["expires_at"] == nil {
		t.Fatalf("body = %v", body)
	}
}

func TestWebhookConfirmationRetriesAreBounded(t *testing.T) {
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusServiceUnavailable) })
	h := newWebhookHarness(t, loopbackClient(), true)
	err := webhookConfirmation{h.channel}.ChallengeChannel(context.Background(),
		domain.NotificationChannel{ID: 7, Destination: rcv.server.URL, SecretEnc: h.repo.target.SecretEnc}, "tok", "en", time.Hour)
	if err == nil || rcv.hits.Load() != webhookConfirmAttempts {
		t.Fatalf("err=%v attempts=%d", err, rcv.hits.Load())
	}
}

func TestWebhookSkipsChannelsThatAreNotActive(t *testing.T) {
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusOK) })
	h := newWebhookHarness(t, loopbackClient(), true)
	h.repo.target = nil
	_ = h.channel.Deliver(context.Background(), sampleDelivery(rcv.server.URL))
	h.channel.Wait()
	if rcv.hits.Load() != 0 || len(h.repo.recorded()) != 0 {
		t.Fatal("an unconfirmed or disabled channel must receive nothing")
	}
}

func TestWebhookUnreadableSecretIsAFailureNotACrash(t *testing.T) {
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusOK) })
	h := newWebhookHarness(t, loopbackClient(), true)
	h.repo.target = &domain.WebhookTarget{ChannelID: 7, SecretEnc: "garbage"}
	_ = h.channel.Deliver(context.Background(), sampleDelivery(rcv.server.URL))
	h.channel.Wait()
	rec := h.repo.recorded()
	if rcv.hits.Load() != 0 || len(rec) != 1 || rec[0].failure != "the signing secret cannot be read" {
		t.Fatalf("hits=%d recorded=%+v", rcv.hits.Load(), rec)
	}
}

func TestWebhookDeliverAfterShutdownIsRefused(t *testing.T) {
	h := newWebhookHarness(t, loopbackClient(), true)
	if err := h.channel.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := h.channel.Deliver(context.Background(), sampleDelivery("https://hooks.example.test")); err == nil {
		t.Fatal("deliver after shutdown must fail")
	}
}

func TestSecretBoxRoundTripAndBinding(t *testing.T) {
	box, err := NewSecretBox("0123456789012345678901234567890123456789")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := box.Seal(1, "whsec_abc")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "whsec_abc") {
		t.Fatal("the secret must not be stored in clear")
	}
	if plain, err := box.Open(1, sealed); err != nil || plain != "whsec_abc" {
		t.Fatalf("open = %q, %v", plain, err)
	}
	if _, err := box.Open(2, sealed); err == nil {
		t.Fatal("a secret must be bound to its channel")
	}
	other, _ := NewSecretBox("another-secret-another-secret-another-secret")
	if _, err := other.Open(1, sealed); err == nil {
		t.Fatal("another master secret must not open it")
	}
	again, _ := box.Seal(1, "whsec_abc")
	if again == sealed {
		t.Fatal("sealing must use a fresh nonce")
	}
	if _, err := NewSecretBox(""); err == nil {
		t.Fatal("an empty master secret must be refused")
	}
	for _, bad := range []string{"", "!!!", "AAAA"} {
		if _, err := box.Open(1, bad); err == nil {
			t.Fatalf("%q must not open", bad)
		}
	}
}

func TestNewWebhookSecretIsRandomAndPrefixed(t *testing.T) {
	a, _ := newWebhookSecret()
	b, _ := newWebhookSecret()
	if !strings.HasPrefix(a, "whsec_") || len(a) != len("whsec_")+64 || a == b {
		t.Fatalf("secrets = %q, %q", a, b)
	}
}

func TestWebhookFormSubmissionEventDescribesTheFormNotATask(t *testing.T) {
	rcv := newReceiver(t, func(_ int, w http.ResponseWriter) { w.WriteHeader(http.StatusNoContent) })
	h := newWebhookHarness(t, loopbackClient(), true)
	delivery := Delivery{
		RecipientID: 3, Locale: "en", Destination: rcv.server.URL + "/hook",
		Content: NotificationContent{
			Type: mustType(domain.NotificationFormSubmitted), WorkspaceID: 5,
			Payload: map[string]any{"form_id": uint(11), "form_name": "Contact us", "submission_id": uint(12)},
		},
	}
	if err := h.channel.Deliver(context.Background(), delivery); err != nil {
		t.Fatal(err)
	}
	h.channel.Wait()
	reqs := rcv.captured()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	var payload map[string]any
	if err := json.Unmarshal(reqs[0].body, &payload); err != nil {
		t.Fatal(err)
	}
	form, _ := payload["form"].(map[string]any)
	if _, hasTask := payload["task"]; hasTask || form["id"] != float64(11) || form["name"] != "Contact us" ||
		form["submission_id"] != float64(12) || form["workspace_id"] != float64(5) ||
		payload["summary"] != `New submission in the form "Contact us"` ||
		payload["link"] != "https://app.example.test/workspaces/5/forms/11/submissions/12" {
		t.Fatalf("payload = %v", payload)
	}
}
