package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"rabbit-hole-server/internal/ratelimit"
)

func TestSubmissionLimiterCapsPerFormAndResets(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limiter := newSubmissionLimiter(ratelimit.NewMemory().WithClock(func() time.Time { return clock }), 2, time.Hour)
	ctx := context.Background()

	for i := 0; i < 2; i++ {
		if !limiter.allow(ctx, 1) {
			t.Fatalf("submission %d must pass", i+1)
		}
	}
	if limiter.allow(ctx, 1) {
		t.Fatal("third submission in the window must be refused")
	}
	if !limiter.allow(ctx, 2) {
		t.Fatal("another form has its own budget")
	}
	clock = clock.Add(time.Hour + time.Second)
	if !limiter.allow(ctx, 1) {
		t.Fatal("budget must reset after the window")
	}
}

func TestSubmissionLimiterDisabledNilAndFailOpen(t *testing.T) {
	ctx := context.Background()
	var nilLimiter *submissionLimiter
	if !nilLimiter.allow(ctx, 1) {
		t.Fatal("nil limiter allows everything")
	}
	if !newSubmissionLimiter(ratelimit.NewMemory(), 0, time.Hour).allow(ctx, 1) {
		t.Fatal("zero max disables the limit")
	}
	if !newSubmissionLimiter(brokenLimiter{}, 1, time.Hour).allow(ctx, 1) {
		t.Fatal("an unavailable store must not block public submissions")
	}
}

type brokenLimiter struct{}

func (brokenLimiter) Allow(context.Context, string, string, int, time.Duration) (bool, time.Duration, error) {
	return false, 0, errors.New("store unavailable")
}
