package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type webhookChannelRepository struct {
	db *gorm.DB
}

func NewWebhookChannelRepository(db *gorm.DB) domain.WebhookChannelRepository {
	return &webhookChannelRepository{db: db}
}

func (r *webhookChannelRepository) SetSecret(ctx context.Context, userID, channelID uint, secretEnc string) error {
	res := database.DBFromContext(ctx, r.db).Exec(
		"UPDATE notification_channels SET secret_enc = ?, failure_count = 0, disabled_at = NULL, last_error = NULL "+
			"WHERE id = ? AND user_id = ? AND kind = ?", secretEnc, channelID, userID, domain.ChannelKindWebhook,
	)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *webhookChannelRepository) ActiveTarget(ctx context.Context, userID uint, destination string) (*domain.WebhookTarget, error) {
	var rows []struct {
		ID        uint
		SecretEnc *string
	}
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT id, secret_enc FROM notification_channels "+
			"WHERE user_id = ? AND kind = ? AND destination = ? AND verified_at IS NOT NULL AND disabled_at IS NULL",
		userID, domain.ChannelKindWebhook, destination,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 || rows[0].SecretEnc == nil {
		return nil, domain.ErrNotFound
	}
	return &domain.WebhookTarget{ChannelID: rows[0].ID, SecretEnc: *rows[0].SecretEnc}, nil
}

func (r *webhookChannelRepository) RecordDelivery(ctx context.Context, channelID uint, failure string, maxFailures int) error {
	if failure == "" {
		return mapErr(database.DBFromContext(ctx, r.db).Exec(
			"UPDATE notification_channels SET failure_count = 0, last_error = NULL WHERE id = ? AND disabled_at IS NULL",
			channelID).Error)
	}
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		"UPDATE notification_channels SET failure_count = failure_count + 1, last_error = ?, "+
			"disabled_at = CASE WHEN failure_count + 1 >= ? THEN now() ELSE NULL END "+
			"WHERE id = ? AND disabled_at IS NULL", failure, maxFailures, channelID,
	).Error)
}
