package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
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
	TokenKindVerify = "verify_email"
	TokenKindReset  = "reset_password"

	ResendInterval = 60 * time.Second

	verifyEmailPath   = "/verify-email"
	resetPasswordPath = "/reset-password"
)

var ErrEmailNotVerified = errors.New("confirm your email address before signing in")

type EmailTokenStore interface {
	Create(ctx context.Context, userID uint, kind, tokenHash string, expiresAt time.Time) error
	LastCreatedAt(ctx context.Context, userID uint, kind string) (*time.Time, error)
	Discard(ctx context.Context, tokenHash string) error
	VerifyEmail(ctx context.Context, tokenHash string) (uint, error)
	ResetPassword(ctx context.Context, tokenHash, passwordHash string) (uint, error)
}

type EmailUserStore interface {
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
}

type AccountEmailConfig struct {
	FrontendURL     string
	VerificationTTL time.Duration
	ResetTTL        time.Duration
}

type AccountEmailService struct {
	users  EmailUserStore
	tokens EmailTokenStore
	sender mail.Sender
	cfg    AccountEmailConfig
	now    func() time.Time
}

func NewAccountEmailService(users EmailUserStore, tokens EmailTokenStore, sender mail.Sender, cfg AccountEmailConfig) *AccountEmailService {
	cfg.FrontendURL = strings.TrimRight(cfg.FrontendURL, "/")
	return &AccountEmailService{users: users, tokens: tokens, sender: sender, cfg: cfg, now: time.Now}
}

func newSecretToken() (token, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate token: %w", err)
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, hashSecretToken(token), nil
}

func hashSecretToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *AccountEmailService) link(path, token string) string {
	return s.cfg.FrontendURL + path + "?token=" + url.QueryEscape(token)
}

func (s *AccountEmailService) issue(ctx context.Context, user *domain.User, kind, path string, ttl time.Duration) error {
	token, hash, err := newSecretToken()
	if err != nil {
		return err
	}
	locale := localeFrom(ctx)
	subject, text, html, err := renderMail(locale, kind, mailView{
		Link: s.link(path, token),
		TTL:  ttlText(locale, ttl),
	})
	if err != nil {
		return fmt.Errorf("render mail: %w", err)
	}
	if err := s.tokens.Create(ctx, user.ID, kind, hash, s.now().Add(ttl)); err != nil {
		return err
	}
	if err := s.sender.Send(ctx, mail.Message{To: user.Email, Subject: subject, Text: text, HTML: html}); err != nil {
		if discardErr := s.tokens.Discard(context.WithoutCancel(ctx), hash); discardErr != nil {
			slog.Warn("account email: cannot discard the token of an unsent mail", "user_id", user.ID, "error", discardErr)
		}
		return err
	}
	return nil
}

func (s *AccountEmailService) coolingDown(ctx context.Context, userID uint, kind string) bool {
	last, err := s.tokens.LastCreatedAt(ctx, userID, kind)
	if err != nil || last == nil {
		return false
	}
	return s.now().Sub(*last) < ResendInterval
}

func (s *AccountEmailService) SendVerification(ctx context.Context, user *domain.User) error {
	return s.issue(ctx, user, TokenKindVerify, verifyEmailPath, s.cfg.VerificationTTL)
}

func (s *AccountEmailService) ResendVerification(ctx context.Context, email string) {
	user, err := s.users.GetByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil || user.EmailVerifiedAt != nil {
		return
	}
	if s.coolingDown(ctx, user.ID, TokenKindVerify) {
		return
	}
	if err := s.SendVerification(ctx, user); err != nil {
		slog.Warn("account email: cannot send a verification mail", "user_id", user.ID, "error", err)
	}
}

func (s *AccountEmailService) VerifyEmail(ctx context.Context, token string) error {
	_, err := s.tokens.VerifyEmail(ctx, hashSecretToken(strings.TrimSpace(token)))
	return err
}

func (s *AccountEmailService) ForgotPassword(ctx context.Context, email string) {
	user, err := s.users.GetByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if err != nil {
		return
	}
	if s.coolingDown(ctx, user.ID, TokenKindReset) {
		return
	}
	if err := s.issue(ctx, user, TokenKindReset, resetPasswordPath, s.cfg.ResetTTL); err != nil {
		slog.Warn("account email: cannot send a password reset mail", "user_id", user.ID, "error", err)
	}
}

func (s *AccountEmailService) ResetPassword(ctx context.Context, token, newPassword string) error {
	if err := validatePassword(newPassword); err != nil {
		return err
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return err
	}
	_, err = s.tokens.ResetPassword(ctx, hashSecretToken(strings.TrimSpace(token)), hash)
	return err
}
