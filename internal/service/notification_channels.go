package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
	mailer "rabbit-hole-server/internal/mail"
	"rabbit-hole-server/internal/ratelimit"
	"rabbit-hole-server/internal/txhook"
)

var ErrEmailThrottled = errors.New("hourly e-mail limit reached")

type NotificationContent struct {
	Type        domain.NotificationType
	TaskID      uint
	TaskTitle   string
	WorkspaceID uint
	Actor       domain.CommentUser
	ActorID     uint
	Payload     map[string]any
}

func (c NotificationContent) Title() string {
	if c.Type.Key == domain.NotificationFormSubmitted {
		return payloadString(c.Payload, "form_name")
	}
	return c.TaskTitle
}

type Delivery struct {
	RecipientID uint
	Locale      string
	Destination string
	Content     NotificationContent
}

type ConfirmationStrategy interface {
	Challenge(ctx context.Context, destination, token, locale string, ttl time.Duration) error
}

type Channel interface {
	Kind() string
	ValidateDestination(raw string) (string, error)
	Confirmation() ConfirmationStrategy
	Destinations(ctx context.Context, userID uint) ([]string, error)
	Deliver(ctx context.Context, d Delivery) error
}

type ChannelRegistry struct {
	ordered []Channel
	byKind  map[string]Channel
}

func NewChannelRegistry(channels ...Channel) *ChannelRegistry {
	r := &ChannelRegistry{byKind: make(map[string]Channel, len(channels))}
	for _, ch := range channels {
		r.ordered = append(r.ordered, ch)
		r.byKind[ch.Kind()] = ch
	}
	return r
}

func (r *ChannelRegistry) All() []Channel { return append([]Channel(nil), r.ordered...) }

func (r *ChannelRegistry) Get(kind string) (Channel, bool) {
	ch, ok := r.byKind[kind]
	return ch, ok
}

type inAppChannel struct {
	repo domain.NotificationRepository
	keep int
	push *notificationPush
}

func NewInAppChannel(repo domain.NotificationRepository, keep int, publisher NotificationPublisher) Channel {
	if keep <= 0 {
		keep = domain.MaxNotificationsPerUser
	}
	return &inAppChannel{repo: repo, keep: keep, push: newNotificationPush(repo, publisher)}
}

func (c *inAppChannel) Kind() string { return domain.ChannelKindInApp }

func (c *inAppChannel) ValidateDestination(string) (string, error) {
	return "", domain.ErrChannelNeedsNoDestination
}

func (c *inAppChannel) Confirmation() ConfirmationStrategy { return nil }

func (c *inAppChannel) Destinations(context.Context, uint) ([]string, error) {
	return []string{domain.ChannelKindInApp}, nil
}

func (c *inAppChannel) Deliver(ctx context.Context, d Delivery) error {
	payload := map[string]any{}
	if d.Content.TaskID != 0 {
		payload["task_title"] = d.Content.TaskTitle
	}
	for k, v := range d.Content.Payload {
		payload[k] = v
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ref, err := c.repo.Create(ctx, domain.NotificationDraft{
		UserID: d.RecipientID, Type: d.Content.Type.Key, TaskID: d.Content.TaskID,
		ActorID: d.Content.ActorID, WorkspaceID: d.Content.WorkspaceID, Payload: raw,
	}, c.keep)
	if err != nil {
		return err
	}
	txhook.AfterCommit(ctx, func() { c.push.created(ctx, d.RecipientID, ref, d.Content) })
	return nil
}

type EmailThrottle struct {
	limiter ratelimit.Limiter
	limit   int
	window  time.Duration
}

func NewEmailThrottle(limiter ratelimit.Limiter, limit int, window time.Duration) *EmailThrottle {
	return &EmailThrottle{limiter: limiter, limit: limit, window: window}
}

func (t *EmailThrottle) Allow(ctx context.Context, userID uint) bool {
	allowed, _, err := t.limiter.Allow(ctx, "notification-email", strconv.FormatUint(uint64(userID), 10), t.limit, t.window)
	if err != nil {
		slog.Warn("notification: cannot check the e-mail limit, sending anyway", "user_id", userID, "error", err)
		return true
	}
	return allowed
}

const maxEmailLength = 254

type emailChannel struct {
	source      domain.NotificationSourceRepository
	channels    domain.NotificationChannelRepository
	sender      mailer.Sender
	frontendURL string
	throttle    *EmailThrottle
}

func NewEmailChannel(
	source domain.NotificationSourceRepository, channels domain.NotificationChannelRepository,
	sender mailer.Sender, frontendURL string, throttle *EmailThrottle,
) Channel {
	return &emailChannel{
		source: source, channels: channels, sender: sender,
		frontendURL: strings.TrimRight(frontendURL, "/"), throttle: throttle,
	}
}

func (c *emailChannel) Kind() string { return domain.ChannelKindEmail }

func (c *emailChannel) ValidateDestination(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || len(value) > maxEmailLength || strings.ContainsAny(value, " \t\r\n<>,;") {
		return "", domain.ErrInvalidDestination
	}
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address != value || !strings.Contains(value[strings.LastIndex(value, "@"):], ".") {
		return "", domain.ErrInvalidDestination
	}
	return value, nil
}

func (c *emailChannel) Confirmation() ConfirmationStrategy { return emailConfirmation{c} }

func (c *emailChannel) Destinations(ctx context.Context, userID uint) ([]string, error) {
	var out []string
	seen := map[string]struct{}{}
	add := func(address string) {
		key := strings.ToLower(address)
		if _, dup := seen[key]; !dup {
			seen[key] = struct{}{}
			out = append(out, address)
		}
	}
	account, err := c.source.AccountMail(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if account.Verified {
		add(account.Email)
	}
	extra, err := c.channels.VerifiedDestinations(ctx, userID, domain.ChannelKindEmail)
	if err != nil {
		return nil, err
	}
	for _, address := range extra {
		add(address)
	}
	return out, nil
}

func (c *emailChannel) Deliver(ctx context.Context, d Delivery) error {
	if !c.throttle.Allow(ctx, d.RecipientID) {
		return ErrEmailThrottled
	}
	subject, text, html, err := renderNotificationMail(d.Locale, c.frontendURL, d.Content)
	if err != nil {
		return fmt.Errorf("render notification mail: %w", err)
	}
	return c.sender.Send(ctx, mailer.Message{To: d.Destination, Subject: subject, Text: text, HTML: html})
}

type emailConfirmation struct {
	c *emailChannel
}

func (e emailConfirmation) Challenge(ctx context.Context, destination, token, locale string, ttl time.Duration) error {
	subject, text, html, err := renderChannelConfirmation(locale, channelMailView{
		Link: e.c.frontendURL + confirmChannelPath + "?token=" + url.QueryEscape(token),
		TTL:  ttlText(locale, ttl),
	})
	if err != nil {
		return fmt.Errorf("render confirmation mail: %w", err)
	}
	return e.c.sender.Send(ctx, mailer.Message{To: destination, Subject: subject, Text: text, HTML: html})
}

func logDeliveryFailure(userID uint, kind string, err error) {
	if errors.Is(err, ErrEmailThrottled) {
		slog.Debug("notification: delivery skipped", "user_id", userID, "channel", kind, "reason", err)
		return
	}
	slog.Warn("notification: cannot deliver", "user_id", userID, "channel", kind, "error", err)
}
