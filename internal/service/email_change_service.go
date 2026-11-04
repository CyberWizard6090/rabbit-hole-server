package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/mail"
)

const (
	TokenKindChangeEmail   = "change_email"
	noticeKindEmailChanged = "email_changed_notice"

	confirmEmailChangePath = "/confirm-email-change"
	EmailChangeTTL         = 24 * time.Hour
)

var (
	ErrSameEmail           = errors.New("the new email address is the same as the current one")
	ErrEmailChangeCooldown = errors.New("a confirmation mail was sent a moment ago, wait a minute before asking again")
)

type EmailChangeStore interface {
	Create(ctx context.Context, userID uint, newEmail, tokenHash string, expiresAt time.Time) error
	Discard(ctx context.Context, tokenHash string) error
	LastCreatedAt(ctx context.Context, userID uint) (*time.Time, error)
	Pending(ctx context.Context, userID uint) (string, error)
	Cancel(ctx context.Context, userID uint) error
	Confirm(ctx context.Context, tokenHash string) (domain.EmailChange, error)
}

type EmailChangeUserStore interface {
	GetByID(ctx context.Context, id uint) (*domain.User, error)
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type EmailChangeService struct {
	users       EmailChangeUserStore
	tokens      EmailChangeStore
	sender      mail.Sender
	frontendURL string
	now         func() time.Time
}

func NewEmailChangeService(users EmailChangeUserStore, tokens EmailChangeStore, sender mail.Sender, frontendURL string) *EmailChangeService {
	return &EmailChangeService{
		users: users, tokens: tokens, sender: sender,
		frontendURL: strings.TrimRight(frontendURL, "/"), now: time.Now,
	}
}

func (s *EmailChangeService) Request(ctx context.Context, userID uint, newEmail, password string) error {
	user, err := s.users.GetByID(ctx, userID)
	if err != nil || !verifyPassword(password, user.PasswordHash) {
		return ErrInvalidCredentials
	}
	newEmail = strings.ToLower(strings.TrimSpace(newEmail))
	if newEmail == strings.ToLower(user.Email) {
		return ErrSameEmail
	}
	if _, err := s.users.GetByEmail(ctx, newEmail); err == nil {
		return domain.ErrEmailTaken
	} else if !errors.Is(err, domain.ErrNotFound) {
		return err
	}
	if last, err := s.tokens.LastCreatedAt(ctx, userID); err == nil && last != nil && s.now().Sub(*last) < ResendInterval {
		return ErrEmailChangeCooldown
	}

	token, hash, err := newSecretToken()
	if err != nil {
		return err
	}
	locale := localeFrom(ctx)
	subject, text, html, err := renderMail(locale, TokenKindChangeEmail, mailView{
		Link: s.frontendURL + confirmEmailChangePath + "?token=" + url.QueryEscape(token),
		TTL:  ttlText(locale, EmailChangeTTL),
	})
	if err != nil {
		return fmt.Errorf("render mail: %w", err)
	}
	if err := s.tokens.Create(ctx, userID, newEmail, hash, s.now().Add(EmailChangeTTL)); err != nil {
		return err
	}
	if err := s.sender.Send(ctx, mail.Message{To: newEmail, Subject: subject, Text: text, HTML: html}); err != nil {
		if discardErr := s.tokens.Discard(context.WithoutCancel(ctx), hash); discardErr != nil {
			slog.Warn("email change: cannot discard the token of an unsent mail", "user_id", userID, "error", discardErr)
		}
		return err
	}
	return nil
}

func (s *EmailChangeService) Confirm(ctx context.Context, token string) error {
	change, err := s.tokens.Confirm(ctx, hashSecretToken(strings.TrimSpace(token)))
	if err != nil {
		return err
	}
	locale := localeFrom(ctx)
	subject, text, html, err := renderMail(locale, noticeKindEmailChanged, mailView{Address: MaskEmail(change.NewEmail)})
	if err != nil {
		slog.Warn("email change: cannot render the security notice", "user_id", change.UserID, "error", err)
		return nil
	}
	if err := s.sender.Send(ctx, mail.Message{To: change.OldEmail, Subject: subject, Text: text, HTML: html}); err != nil {
		slog.Warn("email change: cannot send the security notice", "user_id", change.UserID, "error", err)
	}
	return nil
}

func (s *EmailChangeService) Cancel(ctx context.Context, userID uint) error {
	return s.tokens.Cancel(ctx, userID)
}

func (s *EmailChangeService) Pending(ctx context.Context, userID uint) string {
	address, err := s.tokens.Pending(ctx, userID)
	if err != nil {
		slog.Warn("email change: cannot read the pending address", "user_id", userID, "error", err)
		return ""
	}
	return address
}

func MaskEmail(address string) string {
	at := strings.LastIndex(address, "@")
	if at <= 0 {
		return "***"
	}
	local := []rune(address[:at])
	return string(local[:1]) + "***" + address[at:]
}
