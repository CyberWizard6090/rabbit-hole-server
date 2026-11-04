package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

const (
	TokenKindVerifyEmail   = "verify_email"
	TokenKindResetPassword = "reset_password"
)

type EmailTokenRepository struct {
	db *gorm.DB
}

func NewEmailTokenRepository(db *gorm.DB) *EmailTokenRepository {
	return &EmailTokenRepository{db: db}
}

func (r *EmailTokenRepository) Create(ctx context.Context, userID uint, kind, tokenHash string, expiresAt time.Time) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM email_tokens WHERE user_id = ? AND kind = ?", userID, kind).Error; err != nil {
			return err
		}
		return tx.Exec(
			"INSERT INTO email_tokens (user_id, kind, token_hash, expires_at) VALUES (?, ?, ?, ?)",
			userID, kind, tokenHash, expiresAt,
		).Error
	}))
}

func (r *EmailTokenRepository) Discard(ctx context.Context, tokenHash string) error {
	return mapErr(r.db.WithContext(ctx).Exec("DELETE FROM email_tokens WHERE token_hash = ? AND used_at IS NULL", tokenHash).Error)
}

func (r *EmailTokenRepository) LastCreatedAt(ctx context.Context, userID uint, kind string) (*time.Time, error) {
	var times []time.Time
	err := r.db.WithContext(ctx).Raw(
		"SELECT created_at FROM email_tokens WHERE user_id = ? AND kind = ? ORDER BY created_at DESC LIMIT 1", userID, kind,
	).Scan(&times).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(times) == 0 {
		return nil, nil
	}
	return &times[0], nil
}

func consumeToken(tx *gorm.DB, kind, tokenHash string) (uint, error) {
	var ids []uint
	err := tx.Raw(
		"UPDATE email_tokens SET used_at = now() "+
			"WHERE token_hash = ? AND kind = ? AND used_at IS NULL AND expires_at > now() RETURNING user_id",
		tokenHash, kind,
	).Scan(&ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, domain.ErrInvalidToken
	}
	return ids[0], nil
}

func (r *EmailTokenRepository) VerifyEmail(ctx context.Context, tokenHash string) (uint, error) {
	var userID uint
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		id, err := consumeToken(tx, TokenKindVerifyEmail, tokenHash)
		if err != nil {
			return err
		}
		userID = id
		return tx.Exec(
			"UPDATE users SET email_verified_at = COALESCE(email_verified_at, now()) WHERE id = ?", id,
		).Error
	})
	return userID, mapErr(err)
}

func (r *EmailTokenRepository) ResetPassword(ctx context.Context, tokenHash, passwordHash string) (uint, error) {
	var userID uint
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		id, err := consumeToken(tx, TokenKindResetPassword, tokenHash)
		if err != nil {
			return err
		}
		userID = id
		if err := tx.Exec(
			"UPDATE users SET password_hash = ?, token_version = token_version + 1, "+
				"email_verified_at = COALESCE(email_verified_at, now()) WHERE id = ?", passwordHash, id,
		).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM user_sessions WHERE user_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Exec("DELETE FROM email_tokens WHERE user_id = ? AND kind = ? AND used_at IS NOT NULL", id, TokenKindResetPassword).Error
	})
	return userID, mapErr(err)
}
