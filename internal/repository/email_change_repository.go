package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

const TokenKindChangeEmail = "change_email"

type EmailChangeRepository struct {
	db *gorm.DB
}

func NewEmailChangeRepository(db *gorm.DB) *EmailChangeRepository {
	return &EmailChangeRepository{db: db}
}

func (r *EmailChangeRepository) Create(ctx context.Context, userID uint, newEmail, tokenHash string, expiresAt time.Time) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM email_tokens WHERE user_id = ? AND kind = ?", userID, TokenKindChangeEmail).Error; err != nil {
			return err
		}
		return tx.Exec(
			"INSERT INTO email_tokens (user_id, kind, token_hash, expires_at, payload) VALUES (?, ?, ?, ?, ?)",
			userID, TokenKindChangeEmail, tokenHash, expiresAt, newEmail,
		).Error
	}))
}

func (r *EmailChangeRepository) Discard(ctx context.Context, tokenHash string) error {
	return mapErr(r.db.WithContext(ctx).Exec(
		"DELETE FROM email_tokens WHERE token_hash = ? AND kind = ? AND used_at IS NULL", tokenHash, TokenKindChangeEmail,
	).Error)
}

func (r *EmailChangeRepository) LastCreatedAt(ctx context.Context, userID uint) (*time.Time, error) {
	var times []time.Time
	err := r.db.WithContext(ctx).Raw(
		"SELECT created_at FROM email_tokens WHERE user_id = ? AND kind = ? ORDER BY created_at DESC LIMIT 1", userID, TokenKindChangeEmail,
	).Scan(&times).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(times) == 0 {
		return nil, nil
	}
	return &times[0], nil
}

func (r *EmailChangeRepository) Pending(ctx context.Context, userID uint) (string, error) {
	var addresses []string
	err := r.db.WithContext(ctx).Raw(
		"SELECT payload FROM email_tokens WHERE user_id = ? AND kind = ? AND used_at IS NULL AND expires_at > now() "+
			"ORDER BY created_at DESC LIMIT 1", userID, TokenKindChangeEmail,
	).Scan(&addresses).Error
	if err != nil {
		return "", mapErr(err)
	}
	if len(addresses) == 0 {
		return "", nil
	}
	return addresses[0], nil
}

func (r *EmailChangeRepository) Cancel(ctx context.Context, userID uint) error {
	return mapErr(r.db.WithContext(ctx).Exec(
		"UPDATE email_tokens SET used_at = now(), payload = NULL WHERE user_id = ? AND kind = ? AND used_at IS NULL",
		userID, TokenKindChangeEmail,
	).Error)
}

func (r *EmailChangeRepository) Confirm(ctx context.Context, tokenHash string) (domain.EmailChange, error) {
	var change domain.EmailChange
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			UserID  uint
			Payload string
		}
		if err := tx.Raw(
			"UPDATE email_tokens SET used_at = now() "+
				"WHERE token_hash = ? AND kind = ? AND used_at IS NULL AND expires_at > now() RETURNING user_id, payload",
			tokenHash, TokenKindChangeEmail,
		).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			return domain.ErrInvalidToken
		}
		change.UserID, change.NewEmail = rows[0].UserID, rows[0].Payload

		var old []string
		if err := tx.Raw("SELECT email FROM users WHERE id = ? FOR UPDATE", change.UserID).Scan(&old).Error; err != nil {
			return err
		}
		if len(old) == 0 {
			return domain.ErrInvalidToken
		}
		change.OldEmail = old[0]

		if err := tx.Exec(
			"UPDATE users SET email = ?, email_verified_at = now() WHERE id = ?", change.NewEmail, change.UserID,
		).Error; err != nil {
			if isEmailUniqueViolation(err) {
				return domain.ErrEmailTaken
			}
			return err
		}
		var version int
		if err := revokeAll(tx, change.UserID, &version); err != nil {
			return err
		}
		return tx.Exec(
			"DELETE FROM email_tokens WHERE user_id = ? AND kind IN ('reset_password', 'verify_email', ?)",
			change.UserID, TokenKindChangeEmail,
		).Error
	})
	if err != nil {
		return domain.EmailChange{}, mapErr(err)
	}
	return change, nil
}
