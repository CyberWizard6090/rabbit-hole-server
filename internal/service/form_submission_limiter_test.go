package service

import (
	"testing"
	"time"
)

func TestSubmissionLimiterCapsPerFormAndResets(t *testing.T) {
	clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	limiter := newSubmissionLimiter(2, time.Hour)
	limiter.window.WithClock(func() time.Time { return clock })

	for i := 0; i < 2; i++ {
		if !limiter.allow(1) {
			t.Fatalf("submission %d must pass", i+1)
		}
	}
	if limiter.allow(1) {
		t.Fatal("third submission in the window must be refused")
	}
	if !limiter.allow(2) {
		t.Fatal("another form has its own budget")
	}
	clock = clock.Add(time.Hour + time.Second)
	if !limiter.allow(1) {
		t.Fatal("budget must reset after the window")
	}
}

func TestSubmissionLimiterDisabledAndNil(t *testing.T) {
	var nilLimiter *submissionLimiter
	if !nilLimiter.allow(1) {
		t.Fatal("nil limiter allows everything")
	}
	if !newSubmissionLimiter(0, time.Hour).allow(1) {
		t.Fatal("zero max disables the limit")
	}
}
