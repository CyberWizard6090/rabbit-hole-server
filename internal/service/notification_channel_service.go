package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
)

const ChannelConfirmationTTL = 48 * time.Hour

var ErrConfirmationMailFailed = errors.New("cannot send the confirmation message right now")

type ChannelView struct {
	ID          *uint      `json:"id"`
	Kind        string     `json:"kind"`
	Destination string     `json:"destination,omitempty"`
	Implicit    bool       `json:"implicit"`
	Verified    bool       `json:"verified"`
	VerifiedAt  *time.Time `json:"verified_at"`
	CreatedAt   *time.Time `json:"created_at"`
}

type NotificationChannelService struct {
	repo     domain.NotificationChannelRepository
	source   domain.NotificationSourceRepository
	registry *ChannelRegistry
	now      func() time.Time
}

func NewNotificationChannelService(
	repo domain.NotificationChannelRepository, source domain.NotificationSourceRepository, registry *ChannelRegistry,
) *NotificationChannelService {
	return &NotificationChannelService{repo: repo, source: source, registry: registry, now: time.Now}
}

func channelView(c domain.NotificationChannel) ChannelView {
	id := c.ID
	created := c.CreatedAt
	return ChannelView{
		ID: &id, Kind: c.Kind, Destination: c.Destination, Verified: c.VerifiedAt != nil,
		VerifiedAt: c.VerifiedAt, CreatedAt: &created,
	}
}

func (s *NotificationChannelService) List(ctx context.Context, userID uint) ([]ChannelView, error) {
	rows, err := s.repo.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := []ChannelView{{Kind: domain.ChannelKindInApp, Implicit: true, Verified: true}}
	account, err := s.source.AccountMail(ctx, userID)
	if err != nil {
		return nil, err
	}
	out = append(out, ChannelView{
		Kind: domain.ChannelKindEmail, Destination: account.Email, Implicit: true, Verified: account.Verified,
	})
	for _, row := range rows {
		out = append(out, channelView(row))
	}
	return out, nil
}

func (s *NotificationChannelService) Add(ctx context.Context, userID uint, kind, destination, locale string) (*ChannelView, error) {
	ch, ok := s.registry.Get(kind)
	if !ok {
		return nil, domain.ErrUnknownChannelKind
	}
	strategy := ch.Confirmation()
	if strategy == nil {
		return nil, domain.ErrChannelNeedsNoDestination
	}
	normalized, err := ch.ValidateDestination(destination)
	if err != nil {
		return nil, err
	}
	created, err := s.repo.Create(ctx, userID, kind, normalized, domain.MaxChannelsPerUserKind)
	if err != nil {
		return nil, err
	}
	if err := s.challenge(ctx, strategy, created, locale); err != nil {
		return nil, err
	}
	view := channelView(*created)
	return &view, nil
}

func (s *NotificationChannelService) challenge(
	ctx context.Context, strategy ConfirmationStrategy, channel *domain.NotificationChannel, locale string,
) error {
	token, hash, err := newSecretToken()
	if err != nil {
		return err
	}
	if err := s.repo.SaveConfirmation(ctx, channel.ID, hash, s.now().Add(ChannelConfirmationTTL)); err != nil {
		return err
	}
	if err := strategy.Challenge(ctx, channel.Destination, token, locale, ChannelConfirmationTTL); err != nil {
		if discardErr := s.repo.DiscardConfirmation(context.WithoutCancel(ctx), hash); discardErr != nil {
			slog.Warn("notification channel: cannot discard the token of an unsent mail", "channel_id", channel.ID, "error", discardErr)
		}
		slog.Warn("notification channel: cannot send the confirmation", "channel_id", channel.ID, "error", err)
		return fmt.Errorf("%w", ErrConfirmationMailFailed)
	}
	return nil
}

func (s *NotificationChannelService) Resend(ctx context.Context, userID, channelID uint, locale string) error {
	channel, err := s.repo.Get(ctx, userID, channelID)
	if err != nil {
		return err
	}
	if channel.VerifiedAt != nil {
		return domain.ErrChannelAlreadyConfirmed
	}
	last, err := s.repo.LastConfirmationAt(ctx, channel.ID)
	if err != nil {
		return err
	}
	if last != nil && s.now().Sub(*last) < ResendInterval {
		return domain.ErrConfirmationTooSoon
	}
	ch, ok := s.registry.Get(channel.Kind)
	if !ok || ch.Confirmation() == nil {
		return domain.ErrUnknownChannelKind
	}
	return s.challenge(ctx, ch.Confirmation(), channel, locale)
}

func (s *NotificationChannelService) Delete(ctx context.Context, userID, channelID uint) error {
	return s.repo.Delete(ctx, userID, channelID)
}

func (s *NotificationChannelService) Confirm(ctx context.Context, token string) error {
	return s.repo.Confirm(ctx, hashSecretToken(strings.TrimSpace(token)))
}
