package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/safehttp"
)

const (
	WebhookSignatureHeader = "X-Rabbit-Hole-Signature"
	WebhookEventHeader     = "X-Rabbit-Hole-Event"
	WebhookDeliveryHeader  = "X-Rabbit-Hole-Delivery"
	WebhookTimestampHeader = "X-Rabbit-Hole-Timestamp"

	WebhookConfirmationEvent = "webhook.confirmation"

	webhookMaxAttempts      = 3
	webhookConfirmAttempts  = 2
	webhookBaseBackoff      = 500 * time.Millisecond
	webhookResponseCap      = 4096
	webhookWorkers          = 4
	webhookQueueSize        = 256
	webhookDeliveryDeadline = 45 * time.Second
	webhookMaxURLLength     = 2048
)

type WebhookClient interface {
	Do(req *http.Request) (*http.Response, error)
	ValidateURL(raw string) (*url.URL, error)
}

type WebhookConfig struct {
	Client      WebhookClient
	Repo        domain.WebhookChannelRepository
	Channels    domain.NotificationChannelRepository
	Box         *SecretBox
	FrontendURL string
	AllowHTTP   bool
	MaxFailures int
}

type SecretIssuer interface {
	IssueSecret(ctx context.Context, userID, channelID uint) (string, error)
}

type ChannelChallenger interface {
	ChallengeChannel(ctx context.Context, channel domain.NotificationChannel, token, locale string, ttl time.Duration) error
}

type webhookFailure struct {
	message   string
	retryable bool
	cause     error
}

func (e *webhookFailure) Error() string { return e.message }
func (e *webhookFailure) Unwrap() error { return e.cause }

type webhookChannel struct {
	cfg     WebhookConfig
	backoff time.Duration
	now     func() time.Time

	ctx    context.Context
	cancel context.CancelFunc
	queue  chan Delivery
	mu     sync.RWMutex
	closed bool

	workers sync.WaitGroup
	pending sync.WaitGroup
}

func NewWebhookChannel(cfg WebhookConfig) Channel {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 10
	}
	cfg.FrontendURL = strings.TrimRight(cfg.FrontendURL, "/")
	ctx, cancel := context.WithCancel(context.Background())
	c := &webhookChannel{
		cfg: cfg, backoff: webhookBaseBackoff, now: time.Now,
		ctx: ctx, cancel: cancel, queue: make(chan Delivery, webhookQueueSize),
	}
	for i := 0; i < webhookWorkers; i++ {
		c.workers.Add(1)
		go c.work()
	}
	return c
}

func (c *webhookChannel) Kind() string { return domain.ChannelKindWebhook }

func (c *webhookChannel) ValidateDestination(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > webhookMaxURLLength {
		return "", domain.ErrInvalidDestination
	}
	u, err := c.cfg.Client.ValidateURL(value)
	if err != nil {
		return "", domain.ErrInvalidDestination
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !c.cfg.AllowHTTP) {
		return "", domain.ErrInvalidDestination
	}
	u.Host = strings.ToLower(u.Host)
	return u.String(), nil
}

func (c *webhookChannel) Confirmation() ConfirmationStrategy { return webhookConfirmation{c} }

func (c *webhookChannel) Destinations(ctx context.Context, userID uint) ([]string, error) {
	return c.cfg.Channels.VerifiedDestinations(ctx, userID, domain.ChannelKindWebhook)
}

func (c *webhookChannel) IssueSecret(ctx context.Context, userID, channelID uint) (string, error) {
	secret, err := newWebhookSecret()
	if err != nil {
		return "", err
	}
	sealed, err := c.cfg.Box.Seal(channelID, secret)
	if err != nil {
		return "", err
	}
	if err := c.cfg.Repo.SetSecret(ctx, userID, channelID, sealed); err != nil {
		return "", err
	}
	return secret, nil
}

func (c *webhookChannel) Deliver(_ context.Context, d Delivery) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.closed {
		return errors.New("webhook delivery is shut down")
	}
	c.pending.Add(1)
	select {
	case c.queue <- d:
		return nil
	default:
		c.pending.Done()
		return errors.New("webhook queue is full")
	}
}

func (c *webhookChannel) Wait() { c.pending.Wait() }

func (c *webhookChannel) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.queue)
	}
	c.mu.Unlock()
	done := make(chan struct{})
	go func() {
		c.workers.Wait()
		close(done)
	}()
	select {
	case <-done:
		c.cancel()
		return nil
	case <-ctx.Done():
		c.cancel()
		return ctx.Err()
	}
}

func (c *webhookChannel) work() {
	defer c.workers.Done()
	for d := range c.queue {
		c.handle(d)
		c.pending.Done()
	}
}

func (c *webhookChannel) handle(d Delivery) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("webhook: recovered from a panic", "panic", fmt.Sprint(r))
		}
	}()
	ctx, cancel := context.WithTimeout(c.ctx, webhookDeliveryDeadline)
	defer cancel()
	target, err := c.cfg.Repo.ActiveTarget(ctx, d.RecipientID, d.Destination)
	if err != nil {
		if !errors.Is(err, domain.ErrNotFound) {
			slog.Warn("webhook: cannot load the channel", "user_id", d.RecipientID, "error", err)
		}
		return
	}
	failure := c.send(ctx, d, target)
	if err := c.cfg.Repo.RecordDelivery(context.WithoutCancel(ctx), target.ChannelID, failure, c.cfg.MaxFailures); err != nil {
		slog.Warn("webhook: cannot record the delivery result", "channel_id", target.ChannelID, "error", err)
	}
	if failure != "" {
		slog.Warn("webhook: delivery failed", "channel_id", target.ChannelID, "reason", failure)
	}
}

func (c *webhookChannel) send(ctx context.Context, d Delivery, target *domain.WebhookTarget) string {
	secret, err := c.cfg.Box.Open(target.ChannelID, target.SecretEnc)
	if err != nil {
		return "the signing secret cannot be read"
	}
	deliveryID, err := newDeliveryID()
	if err != nil {
		return "internal error"
	}
	body, err := json.Marshal(c.eventBody(d, deliveryID))
	if err != nil {
		return "internal error"
	}
	err = c.post(ctx, d.Destination, d.Content.Type.Key, deliveryID, body, secret, webhookMaxAttempts)
	if err != nil {
		return sanitizedFailure(err)
	}
	return ""
}

func sanitizedFailure(err error) string {
	var failure *webhookFailure
	if errors.As(err, &failure) {
		return failure.message
	}
	return "delivery failed"
}

type webhookTask struct {
	ID          uint   `json:"id"`
	Title       string `json:"title"`
	WorkspaceID uint   `json:"workspace_id"`
}

type webhookActor struct {
	DisplayName string `json:"display_name"`
}

type webhookEvent struct {
	Type       string       `json:"type"`
	DeliveryID string       `json:"delivery_id"`
	CreatedAt  time.Time    `json:"created_at"`
	Summary    string       `json:"summary"`
	Detail     string       `json:"detail,omitempty"`
	Task       webhookTask  `json:"task"`
	Actor      webhookActor `json:"actor"`
	Link       string       `json:"link"`
}

func (c *webhookChannel) eventBody(d Delivery, deliveryID string) webhookEvent {
	content := d.Content
	locale := d.Locale
	if !domain.IsSupportedNotificationLocale(locale) {
		locale = domain.DefaultNotificationLocale
	}
	strs := notificationMailLocales[locale]
	actor := actorDisplayName(content.Actor, strs.SomeoneName)
	title := singleLine(content.TaskTitle)
	return webhookEvent{
		Type: content.Type.Key, DeliveryID: deliveryID, CreatedAt: c.now().UTC(),
		Summary: singleLine(fmt.Sprintf(content.Type.WordingFor(locale).Subject, actor, title)),
		Detail:  notificationDetail(locale, content.Type.Key, content.Payload),
		Task:    webhookTask{ID: content.TaskID, Title: title, WorkspaceID: content.WorkspaceID},
		Actor:   webhookActor{DisplayName: actor},
		Link:    c.cfg.FrontendURL + taskPathPrefix + strconv.FormatUint(uint64(content.TaskID), 10),
	}
}

type webhookConfirmationBody struct {
	Type              string    `json:"type"`
	DeliveryID        string    `json:"delivery_id"`
	CreatedAt         time.Time `json:"created_at"`
	ConfirmationToken string    `json:"confirmation_token"`
	ConfirmURL        string    `json:"confirm_url"`
	ExpiresAt         time.Time `json:"expires_at"`
}

type webhookConfirmation struct {
	c *webhookChannel
}

func (webhookConfirmation) Challenge(context.Context, string, string, string, time.Duration) error {
	return errors.New("a webhook confirmation needs the channel")
}

func (w webhookConfirmation) ChallengeChannel(
	ctx context.Context, channel domain.NotificationChannel, token, _ string, ttl time.Duration,
) error {
	c := w.c
	secret, err := c.cfg.Box.Open(channel.ID, channel.SecretEnc)
	if err != nil {
		return err
	}
	deliveryID, err := newDeliveryID()
	if err != nil {
		return err
	}
	now := c.now().UTC()
	body, err := json.Marshal(webhookConfirmationBody{
		Type: WebhookConfirmationEvent, DeliveryID: deliveryID, CreatedAt: now, ConfirmationToken: token,
		ConfirmURL: c.cfg.FrontendURL + confirmChannelPath + "?token=" + url.QueryEscape(token),
		ExpiresAt:  now.Add(ttl),
	})
	if err != nil {
		return err
	}
	return c.post(ctx, channel.Destination, WebhookConfirmationEvent, deliveryID, body, secret, webhookConfirmAttempts)
}

func SignWebhook(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (c *webhookChannel) post(
	ctx context.Context, target, event, deliveryID string, body []byte, secret string, attempts int,
) error {
	var last error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(c.backoff << (attempt - 1))
			select {
			case <-ctx.Done():
				timer.Stop()
				return last
			case <-timer.C:
			}
		}
		err := c.attempt(ctx, target, event, deliveryID, body, secret)
		if err == nil {
			return nil
		}
		last = err
		var failure *webhookFailure
		if !errors.As(err, &failure) || !failure.retryable {
			return err
		}
	}
	return last
}

func (c *webhookChannel) attempt(ctx context.Context, target, event, deliveryID string, body []byte, secret string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return &webhookFailure{message: "the destination url is not valid", cause: domain.ErrInvalidDestination}
	}
	timestamp := strconv.FormatInt(c.now().Unix(), 10)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(WebhookEventHeader, event)
	req.Header.Set(WebhookDeliveryHeader, deliveryID)
	req.Header.Set(WebhookTimestampHeader, timestamp)
	req.Header.Set(WebhookSignatureHeader, SignWebhook(secret, timestamp, body))

	resp, err := c.cfg.Client.Do(req)
	if err != nil {
		return classifyWebhookError(err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, webhookResponseCap))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 500 || resp.StatusCode == http.StatusTooManyRequests:
		return &webhookFailure{message: "the endpoint answered HTTP " + strconv.Itoa(resp.StatusCode), retryable: true}
	default:
		return &webhookFailure{message: "the endpoint answered HTTP " + strconv.Itoa(resp.StatusCode)}
	}
}

func classifyWebhookError(err error) error {
	var netErr net.Error
	switch {
	case errors.Is(err, safehttp.ErrForbiddenURL), errors.Is(err, safehttp.ErrForbiddenAddress):
		return &webhookFailure{message: "the destination address is not allowed", cause: domain.ErrInvalidDestination}
	case errors.Is(err, safehttp.ErrTooManyRedirects):
		return &webhookFailure{message: "the endpoint redirected the request, redirects are not followed"}
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &netErr) && netErr.Timeout():
		return &webhookFailure{message: "the request timed out", retryable: true}
	default:
		return &webhookFailure{message: "the endpoint cannot be reached", retryable: true}
	}
}
