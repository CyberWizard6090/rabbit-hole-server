package service

import (
	"strconv"
	"time"

	"rabbit-hole-server/internal/ratelimit"
)

type submissionLimiter struct {
	window *ratelimit.Window
}

func newSubmissionLimiter(max int, period time.Duration) *submissionLimiter {
	return &submissionLimiter{window: ratelimit.NewWindow(max, period)}
}

func (l *submissionLimiter) allow(formID uint) bool {
	if l == nil {
		return true
	}
	allowed, _ := l.window.Allow(strconv.FormatUint(uint64(formID), 10))
	return allowed
}
