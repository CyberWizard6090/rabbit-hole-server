package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

const retentionBatch = 1000

type retentionRepository struct {
	db *gorm.DB
}

func NewRetentionRepository(db *gorm.DB) domain.RetentionRepository {
	return &retentionRepository{db: db}
}

func (r *retentionRepository) purgeBatches(ctx context.Context, query string, args ...any) (int64, error) {
	var total int64
	for {
		result := database.DBFromContext(ctx, r.db).Exec(query, append(args, retentionBatch)...)
		if result.Error != nil {
			return total, mapErr(result.Error)
		}
		total += result.RowsAffected
		if result.RowsAffected < retentionBatch || ctx.Err() != nil {
			return total, nil
		}
	}
}

func seconds(d time.Duration) float64 { return d.Seconds() }

func (r *retentionRepository) Purge(ctx context.Context, policy domain.RetentionPolicy) (domain.RetentionResult, error) {
	var result domain.RetentionResult
	var err error
	if policy.OutboxDone > 0 {
		result.OutboxDone, err = r.purgeBatches(ctx, `
			DELETE FROM outbox_events WHERE id IN (
				SELECT id FROM outbox_events WHERE published_at < now() - make_interval(secs => ?)
				ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED)`, seconds(policy.OutboxDone))
		if err != nil {
			return result, err
		}
	}
	if policy.OutboxDead > 0 {
		result.OutboxDead, err = r.purgeBatches(ctx, `
			DELETE FROM outbox_events WHERE id IN (
				SELECT id FROM outbox_events WHERE dead_at < now() - make_interval(secs => ?)
				ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED)`, seconds(policy.OutboxDead))
		if err != nil {
			return result, err
		}
	}
	if policy.ExpiredTokens <= 0 {
		return result, nil
	}
	grace := seconds(policy.ExpiredTokens)
	if result.EmailTokens, err = r.purgeBatches(ctx, `
		DELETE FROM email_tokens WHERE id IN (
			SELECT id FROM email_tokens
			WHERE expires_at < now() - make_interval(secs => ?) OR used_at < now() - make_interval(secs => ?)
			ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED)`, grace, grace); err != nil {
		return result, err
	}
	if result.Confirmations, err = r.purgeBatches(ctx, `
		DELETE FROM channel_confirmations WHERE id IN (
			SELECT id FROM channel_confirmations
			WHERE expires_at < now() - make_interval(secs => ?) OR used_at < now() - make_interval(secs => ?)
			ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED)`, grace, grace); err != nil {
		return result, err
	}
	if result.Sessions, err = r.purgeBatches(ctx, `
		DELETE FROM user_sessions WHERE id IN (
			SELECT id FROM user_sessions WHERE expires_at < now() - make_interval(secs => ?)
			ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED)`, grace); err != nil {
		return result, err
	}
	result.Idempotency, err = r.purgeBatches(ctx, `
		DELETE FROM form_idempotency_keys WHERE id IN (
			SELECT id FROM form_idempotency_keys WHERE expires_at < now() - make_interval(secs => ?)
			ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED)`, grace)
	return result, err
}
