package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type notificationSettingsRepository struct {
	db *gorm.DB
}

func NewNotificationSettingsRepository(db *gorm.DB) domain.NotificationSettingsRepository {
	return &notificationSettingsRepository{db: db}
}

func (r *notificationSettingsRepository) ListPreferences(ctx context.Context, userID uint) ([]domain.NotificationPreference, error) {
	var rows []struct {
		Type    string
		Channel string
		Enabled bool
	}
	err := r.db.WithContext(ctx).Raw(
		"SELECT type, channel, enabled FROM notification_preferences WHERE user_id = ?", userID,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.NotificationPreference, len(rows))
	for i, row := range rows {
		out[i] = domain.NotificationPreference{Type: row.Type, Channel: row.Channel, Enabled: row.Enabled}
	}
	return out, nil
}

func (r *notificationSettingsRepository) SavePreferences(ctx context.Context, userID uint, prefs []domain.NotificationPreference) error {
	return mapErr(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, p := range prefs {
			if err := tx.Exec(
				"INSERT INTO notification_preferences (user_id, type, channel, enabled) VALUES (?, ?, ?, ?) "+
					"ON CONFLICT (user_id, type, channel) DO UPDATE SET enabled = EXCLUDED.enabled",
				userID, p.Type, p.Channel, p.Enabled,
			).Error; err != nil {
				return err
			}
		}
		return nil
	}))
}

func (r *notificationSettingsRepository) Locale(ctx context.Context, userID uint) (string, error) {
	var locales []string
	err := r.db.WithContext(ctx).Raw("SELECT locale FROM notification_settings WHERE user_id = ?", userID).Scan(&locales).Error
	if err != nil {
		return "", mapErr(err)
	}
	if len(locales) == 0 {
		return domain.DefaultNotificationLocale, nil
	}
	return locales[0], nil
}

func (r *notificationSettingsRepository) SaveLocale(ctx context.Context, userID uint, locale string) error {
	return mapErr(r.db.WithContext(ctx).Exec(
		"INSERT INTO notification_settings (user_id, locale) VALUES (?, ?) "+
			"ON CONFLICT (user_id) DO UPDATE SET locale = EXCLUDED.locale, updated_at = now()", userID, locale,
	).Error)
}
