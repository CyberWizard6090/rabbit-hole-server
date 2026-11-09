package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/ratelimit"
)

type rateLimitRepository struct {
	db *gorm.DB
}

func NewRateLimiter(db *gorm.DB) *rateLimitRepository {
	return &rateLimitRepository{db: db}
}

type windowResult struct {
	Count       int
	WaitSeconds float64
}

func (r *rateLimitRepository) Allow(
	ctx context.Context, bucket, key string, max int, period time.Duration,
) (bool, time.Duration, error) {
	if max <= 0 {
		return true, 0, nil
	}
	var result windowResult
	err := database.DBFromContext(ctx, r.db).Raw(`
		INSERT INTO rate_limit_windows (bucket, key, count, reset_at)
		VALUES (?, ?, 1, now() + make_interval(secs => ?))
		ON CONFLICT (bucket, key) DO UPDATE SET
			count = CASE WHEN rate_limit_windows.reset_at <= now() THEN 1 ELSE rate_limit_windows.count + 1 END,
			reset_at = CASE WHEN rate_limit_windows.reset_at <= now()
				THEN now() + make_interval(secs => ?) ELSE rate_limit_windows.reset_at END
		RETURNING count, EXTRACT(EPOCH FROM (reset_at - now()))::float8 AS wait_seconds`,
		bucket, key, period.Seconds(), period.Seconds(),
	).Scan(&result).Error
	if err != nil {
		return false, 0, mapErr(err)
	}
	if result.Count <= max {
		return true, 0, nil
	}
	return false, time.Duration(result.WaitSeconds * float64(time.Second)), nil
}

func (r *rateLimitRepository) PurgeExpired(ctx context.Context, forget time.Duration) error {
	db := database.DBFromContext(ctx, r.db)
	if err := db.Exec(`DELETE FROM rate_limit_windows WHERE reset_at < now() - interval '1 minute'`).Error; err != nil {
		return mapErr(err)
	}
	return mapErr(db.Exec(
		`DELETE FROM login_lockouts WHERE last_fail_at < now() - make_interval(secs => ?)`, forget.Seconds(),
	).Error)
}

type lockoutRepository struct {
	db  *gorm.DB
	cfg ratelimit.BackoffConfig
}

func NewLockoutStore(db *gorm.DB, cfg ratelimit.BackoffConfig) *lockoutRepository {
	if cfg.Forget <= 0 {
		cfg.Forget = time.Hour
	}
	return &lockoutRepository{db: db, cfg: cfg}
}

func (r *lockoutRepository) Enabled() bool { return r != nil && r.cfg.Threshold > 0 }

func (r *lockoutRepository) Locked(ctx context.Context, subject string) (bool, time.Duration, error) {
	if !r.Enabled() {
		return false, 0, nil
	}
	var waits []float64
	err := database.DBFromContext(ctx, r.db).Raw(
		`SELECT EXTRACT(EPOCH FROM (locked_until - now()))::float8 FROM login_lockouts WHERE subject = ? AND locked_until > now()`,
		ratelimit.SubjectKey(subject),
	).Scan(&waits).Error
	if err != nil {
		return false, 0, mapErr(err)
	}
	if len(waits) == 0 {
		return false, 0, nil
	}
	return true, time.Duration(waits[0] * float64(time.Second)), nil
}

func (r *lockoutRepository) Fail(ctx context.Context, subject string) (time.Duration, error) {
	if !r.Enabled() {
		return 0, nil
	}
	db := database.DBFromContext(ctx, r.db)
	key := ratelimit.SubjectKey(subject)
	var failures int
	err := db.Raw(`
		INSERT INTO login_lockouts (subject, failures, last_fail_at) VALUES (?, 1, now())
		ON CONFLICT (subject) DO UPDATE SET
			failures = CASE WHEN login_lockouts.last_fail_at < now() - make_interval(secs => ?)
				THEN 1 ELSE login_lockouts.failures + 1 END,
			last_fail_at = now()
		RETURNING failures`, key, r.cfg.Forget.Seconds(),
	).Scan(&failures).Error
	if err != nil {
		return 0, mapErr(err)
	}
	delay := ratelimit.LockDelay(r.cfg, failures)
	if delay <= 0 {
		return 0, nil
	}
	err = db.Exec(
		`UPDATE login_lockouts SET locked_until = now() + make_interval(secs => ?) WHERE subject = ?`,
		delay.Seconds(), key,
	).Error
	return delay, mapErr(err)
}

func (r *lockoutRepository) Succeed(ctx context.Context, subject string) error {
	if !r.Enabled() {
		return nil
	}
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		`DELETE FROM login_lockouts WHERE subject = ?`, ratelimit.SubjectKey(subject),
	).Error)
}
