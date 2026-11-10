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
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/safehttp"
	"rabbit-hole-server/internal/txhook"
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
	webhookDeliveryDeadline = 45 * time.Second
	webhookMaxURLLength     = 2048
	webhookOutboxAttempts   = 6
)

var webhookOutboxBackoff = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour}

type WebhookClient interface {
	Do(req *http.Request) (*http.Response, error)
	ValidateURL(raw string) (*url.URL, error)
}

type WebhookConfig struct {
	Client      WebhookClient
	Repo        domain.WebhookChannelRepository
	Channels    domain.NotificationChannelRepository
	Box         *SecretBox
	Outbox      domain.OutboxRepository
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
	poke    func()
}

func NewWebhookChannel(cfg WebhookConfig) Channel {
	if cfg.MaxFailures <= 0 {
		cfg.MaxFailures = 10
	}
	cfg.FrontendURL = strings.TrimRight(cfg.FrontendURL, "/")
	return &webhookChannel{cfg: cfg, backoff: webhookBaseBackoff, now: time.Now}
}

func (c *webhookChannel) AttachWorker(worker *OutboxWorker) {
	c.poke = worker.Poke
	worker.Register(OutboxRoute{
		Type: domain.OutboxDeliveryWebhook, Handler: OutboxHandlerFunc(c.handleEvent),
		MaxAttempts: webhookOutboxAttempts, Backoff: webhookOutboxBackoff,
	})
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

type webhookJob struct {
	RecipientID uint               `json:"recipient_id"`
	Locale      string             `json:"locale"`
	Destination string             `json:"destination"`
	TypeKey     string             `json:"type"`
	TaskID      uint               `json:"task_id"`
	TaskTitle   string             `json:"task_title"`
	WorkspaceID uint               `json:"workspace_id"`
	Actor       domain.CommentUser `json:"actor"`
	ActorID     uint               `json:"actor_id"`
	Payload     map[string]any     `json:"payload"`
}

func (c *webhookChannel) Deliver(ctx context.Context, d Delivery) error {
	raw, err := json.Marshal(webhookJob{
		RecipientID: d.RecipientID, Locale: d.Locale, Destination: d.Destination, TypeKey: d.Content.Type.Key,
		TaskID: d.Content.TaskID, TaskTitle: d.Content.TaskTitle, WorkspaceID: d.Content.WorkspaceID,
		Actor: d.Content.Actor, ActorID: d.Content.ActorID, Payload: d.Content.Payload,
	})
	if err != nil {
		return err
	}
	if err := c.cfg.Outbox.Add(ctx, []domain.OutboxEvent{{
		Type: domain.OutboxDeliveryWebhook, AggregateType: "user", AggregateID: d.RecipientID, Payload: raw,
	}}); err != nil {
		return err
	}
	if c.poke != nil {
		txhook.AfterCommit(ctx, c.poke)
	}
	return nil
}

func (c *webhookChannel) handleEvent(ctx context.Context, event domain.OutboxEvent, final bool) error {
	var job webhookJob
	if err := json.Unmarshal(event.Payload, &job); err != nil {
		return PermanentOutboxError(fmt.Errorf("decode webhook delivery: %w", err))
	}
	typeDef, ok := domain.NotificationTypeByKey(job.TypeKey)
	if !ok {
		return PermanentOutboxError(fmt.Errorf("unknown notification type %q", job.TypeKey))
	}
	d := Delivery{
		RecipientID: job.RecipientID, Locale: job.Locale, Destination: job.Destination,
		Content: NotificationContent{
			Type: typeDef, TaskID: job.TaskID, TaskTitle: job.TaskTitle, WorkspaceID: job.WorkspaceID,
			Actor: job.Actor, ActorID: job.ActorID, Payload: job.Payload,
		},
	}
	ctx, cancel := context.WithTimeout(ctx, webhookDeliveryDeadline)
	defer cancel()
	target, err := c.cfg.Repo.ActiveTarget(ctx, d.RecipientID, d.Destination)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil
		}
		return err
	}
	failure := c.send(ctx, d, target)
	if failure == nil {
		return c.record(ctx, target.ChannelID, "")
	}
	if failure.retryable && !final {
		return failure
	}
	if err := c.record(ctx, target.ChannelID, failure.message); err != nil {
		return err
	}
	slog.Warn("webhook: delivery failed", "channel_id", target.ChannelID, "reason", failure.message)
	return PermanentOutboxError(failure)
}

func (c *webhookChannel) record(ctx context.Context, channelID uint, failure string) error {
	return c.cfg.Repo.RecordDelivery(context.WithoutCancel(ctx), channelID, failure, c.cfg.MaxFailures)
}

func (c *webhookChannel) send(ctx context.Context, d Delivery, target *domain.WebhookTarget) *webhookFailure {
	secret, err := c.cfg.Box.Open(target.ChannelID, target.SecretEnc)
	if err != nil {
		return &webhookFailure{message: "the signing secret cannot be read", cause: err}
	}
	deliveryID, err := newDeliveryID()
	if err != nil {
		return &webhookFailure{message: "internal error", cause: err, retryable: true}
	}
	body, err := json.Marshal(c.eventBody(d, deliveryID))
	if err != nil {
		return &webhookFailure{message: "internal error", cause: err}
	}
	err = c.post(ctx, d.Destination, d.Content.Type.Key, deliveryID, body, secret, webhookMaxAttempts)
	if err == nil {
		return nil
	}
	var failure *webhookFailure
	if errors.As(err, &failure) {
		return failure
	}
	return &webhookFailure{message: "delivery failed", cause: err, retryable: true}
}

type webhookTask struct {
	ID          uint   `json:"id"`
	Title       string `json:"title"`
	WorkspaceID uint   `json:"workspace_id"`
}

type webhookForm struct {
	ID           uint   `json:"id"`
	Name         string `json:"name"`
	SubmissionID uint   `json:"submission_id"`
	WorkspaceID  uint   `json:"workspace_id"`
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
	Task       *webhookTask `json:"task,omitempty"`
	Form       *webhookForm `json:"form,omitempty"`
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
	title := singleLine(content.Title())
	event := webhookEvent{
		Type: content.Type.Key, DeliveryID: deliveryID, CreatedAt: c.now().UTC(),
		Summary: singleLine(fmt.Sprintf(content.Type.WordingFor(locale).Subject, actor, title)),
		Detail:  notificationDetail(locale, content.Type.Key, content.Payload),
		Actor:   webhookActor{DisplayName: actor},
		Link:    notificationLink(c.cfg.FrontendURL, content),
	}
	if content.Type.Key == domain.NotificationFormSubmitted {
		event.Form = &webhookForm{
			ID: payloadID(content.Payload, "form_id"), Name: title,
			SubmissionID: payloadID(content.Payload, "submission_id"), WorkspaceID: content.WorkspaceID,
		}
		return event
	}
	event.Task = &webhookTask{ID: content.TaskID, Title: title, WorkspaceID: content.WorkspaceID}
	return event
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
