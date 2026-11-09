package service

import (
	"context"
	"log/slog"
	"strconv"
	"time"

	"rabbit-hole-server/internal/ratelimit"
)

type submissionLimiter struct {
	limiter ratelimit.Limiter
	max     int
	period  time.Duration
}

func newSubmissionLimiter(limiter ratelimit.Limiter, max int, period time.Duration) *submissionLimiter {
	return &submissionLimiter{limiter: limiter, max: max, period: period}
}

func (l *submissionLimiter) allow(ctx context.Context, formID uint) bool {
	if l == nil || l.limiter == nil {
		return true
	}
	allowed, _, err := l.limiter.Allow(ctx, "form-submissions", strconv.FormatUint(uint64(formID), 10), l.max, l.period)
	if err != nil {
		slog.Warn("forms: cannot check the submission limit, accepting the submission", "form_id", formID, "error", err)
		return true
	}
	return allowed
}
