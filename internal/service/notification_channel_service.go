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
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	Secret      string     `json:"secret,omitempty"`
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
		VerifiedAt: c.VerifiedAt, CreatedAt: &created, DisabledAt: c.DisabledAt, LastError: c.LastError,
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
	issuer, issuesSecret := ch.(SecretIssuer)
	var secret string
	if issuesSecret {
		channelID := created.ID
		secret, err = issuer.IssueSecret(ctx, userID, channelID)
		if err == nil {
			created, err = s.repo.Get(ctx, userID, channelID)
		}
		if err != nil {
			s.rollback(ctx, userID, channelID)
			return nil, err
		}
	}
	if err := s.challenge(ctx, strategy, created, locale); err != nil {
		if issuesSecret {
			s.rollback(ctx, userID, created.ID)
		}
		return nil, err
	}
	view := channelView(*created)
	view.Secret = secret
	return &view, nil
}

func (s *NotificationChannelService) rollback(ctx context.Context, userID, channelID uint) {
	if err := s.repo.Delete(context.WithoutCancel(ctx), userID, channelID); err != nil && !errors.Is(err, domain.ErrNotFound) {
		slog.Warn("notification channel: cannot remove a channel that failed to start", "channel_id", channelID, "error", err)
	}
}

func (s *NotificationChannelService) RotateSecret(ctx context.Context, userID, channelID uint) (string, error) {
	channel, err := s.repo.Get(ctx, userID, channelID)
	if err != nil {
		return "", err
	}
	ch, ok := s.registry.Get(channel.Kind)
	if !ok {
		return "", domain.ErrUnknownChannelKind
	}
	issuer, ok := ch.(SecretIssuer)
	if !ok {
		return "", domain.ErrChannelHasNoSecret
	}
	return issuer.IssueSecret(ctx, userID, channelID)
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
	var sendErr error
	if challenger, ok := strategy.(ChannelChallenger); ok {
		sendErr = challenger.ChallengeChannel(ctx, *channel, token, locale, ChannelConfirmationTTL)
	} else {
		sendErr = strategy.Challenge(ctx, channel.Destination, token, locale, ChannelConfirmationTTL)
	}
	if err := sendErr; err != nil {
		if discardErr := s.repo.DiscardConfirmation(context.WithoutCancel(ctx), hash); discardErr != nil {
			slog.Warn("notification channel: cannot discard the token of an unsent mail", "channel_id", channel.ID, "error", discardErr)
		}
		slog.Warn("notification channel: cannot send the confirmation", "channel_id", channel.ID, "error", err)
		if errors.Is(err, domain.ErrInvalidDestination) {
			return domain.ErrInvalidDestination
		}
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
