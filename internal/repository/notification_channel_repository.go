package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type notificationChannelRepository struct {
	db *gorm.DB
}

func NewNotificationChannelRepository(db *gorm.DB) domain.NotificationChannelRepository {
	return &notificationChannelRepository{db: db}
}

type channelRow struct {
	ID          uint
	Kind        string
	Destination string
	VerifiedAt  *time.Time
	CreatedAt   time.Time
	DisabledAt  *time.Time
	LastError   *string
	SecretEnc   *string
}

const channelColumns = "id, kind, destination, verified_at, created_at, disabled_at, last_error, secret_enc"

func (c channelRow) toDomain() domain.NotificationChannel {
	out := domain.NotificationChannel{
		ID: c.ID, Kind: c.Kind, Destination: c.Destination, VerifiedAt: c.VerifiedAt, CreatedAt: c.CreatedAt,
		DisabledAt: c.DisabledAt,
	}
	if c.LastError != nil {
		out.LastError = *c.LastError
	}
	if c.SecretEnc != nil {
		out.SecretEnc = *c.SecretEnc
	}
	return out
}

func (r *notificationChannelRepository) Create(
	ctx context.Context, userID uint, kind, destination string, maxPerKind int,
) (*domain.NotificationChannel, error) {
	var created channelRow
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT id FROM users WHERE id = ? FOR UPDATE", userID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Raw(
			"SELECT count(*) FROM notification_channels WHERE user_id = ? AND kind = ?", userID, kind,
		).Scan(&count).Error; err != nil {
			return err
		}
		if count >= int64(maxPerKind) {
			return domain.ErrChannelLimitReached
		}
		var rows []channelRow
		if err := tx.Raw(
			"INSERT INTO notification_channels (user_id, kind, destination) VALUES (?, ?, ?) "+
				"RETURNING "+channelColumns, userID, kind, destination,
		).Scan(&rows).Error; err != nil {
			return err
		}
		created = rows[0]
		return nil
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, domain.ErrChannelExists
		}
		return nil, mapErr(err)
	}
	out := created.toDomain()
	return &out, nil
}

func (r *notificationChannelRepository) List(ctx context.Context, userID uint) ([]domain.NotificationChannel, error) {
	var rows []channelRow
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT "+channelColumns+" FROM notification_channels WHERE user_id = ? ORDER BY id", userID,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.NotificationChannel, len(rows))
	for i, row := range rows {
		out[i] = row.toDomain()
	}
	return out, nil
}

func (r *notificationChannelRepository) Get(ctx context.Context, userID, id uint) (*domain.NotificationChannel, error) {
	var rows []channelRow
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT "+channelColumns+" FROM notification_channels WHERE id = ? AND user_id = ?", id, userID,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	out := rows[0].toDomain()
	return &out, nil
}

func (r *notificationChannelRepository) Delete(ctx context.Context, userID, id uint) error {
	res := database.DBFromContext(ctx, r.db).Exec("DELETE FROM notification_channels WHERE id = ? AND user_id = ?", id, userID)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *notificationChannelRepository) VerifiedDestinations(ctx context.Context, userID uint, kind string) ([]string, error) {
	var out []string
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT destination FROM notification_channels WHERE user_id = ? AND kind = ? AND verified_at IS NOT NULL AND disabled_at IS NULL ORDER BY id",
		userID, kind,
	).Scan(&out).Error
	return out, mapErr(err)
}

func (r *notificationChannelRepository) SaveConfirmation(ctx context.Context, channelID uint, tokenHash string, expiresAt time.Time) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("DELETE FROM channel_confirmations WHERE channel_id = ? AND used_at IS NULL", channelID).Error; err != nil {
			return err
		}
		return tx.Exec(
			"INSERT INTO channel_confirmations (channel_id, token_hash, expires_at) VALUES (?, ?, ?)",
			channelID, tokenHash, expiresAt,
		).Error
	}))
}

func (r *notificationChannelRepository) DiscardConfirmation(ctx context.Context, tokenHash string) error {
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		"DELETE FROM channel_confirmations WHERE token_hash = ? AND used_at IS NULL", tokenHash).Error)
}

func (r *notificationChannelRepository) LastConfirmationAt(ctx context.Context, channelID uint) (*time.Time, error) {
	var times []time.Time
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT created_at FROM channel_confirmations WHERE channel_id = ? ORDER BY created_at DESC LIMIT 1", channelID,
	).Scan(&times).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(times) == 0 {
		return nil, nil
	}
	return &times[0], nil
}

func (r *notificationChannelRepository) Confirm(ctx context.Context, tokenHash string) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var ids []uint
		if err := tx.Raw(
			"UPDATE channel_confirmations SET used_at = now() "+
				"WHERE token_hash = ? AND used_at IS NULL AND expires_at > now() RETURNING channel_id", tokenHash,
		).Scan(&ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return domain.ErrInvalidToken
		}
		return tx.Exec(
			"UPDATE notification_channels SET verified_at = COALESCE(verified_at, now()) WHERE id = ?", ids[0],
		).Error
	}))
}
