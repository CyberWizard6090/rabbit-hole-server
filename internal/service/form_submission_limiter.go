package service

import (
	"sync"
	"time"
)

type submissionLimiter struct {
	mu      sync.Mutex
	max     int
	period  time.Duration
	now     func() time.Time
	windows map[uint]*submissionWindow
	sweepAt time.Time
}

type submissionWindow struct {
	count   int
	resetAt time.Time
}

func newSubmissionLimiter(max int, period time.Duration) *submissionLimiter {
	return &submissionLimiter{max: max, period: period, now: time.Now, windows: map[uint]*submissionWindow{}}
}

func (l *submissionLimiter) allow(formID uint) bool {
	if l == nil || l.max <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if l.sweepAt.IsZero() {
		l.sweepAt = now.Add(l.period)
	}
	if now.After(l.sweepAt) {
		for id, w := range l.windows {
			if now.After(w.resetAt) {
				delete(l.windows, id)
			}
		}
		l.sweepAt = now.Add(l.period)
	}
	w, ok := l.windows[formID]
	if !ok || now.After(w.resetAt) {
		w = &submissionWindow{resetAt: now.Add(l.period)}
		l.windows[formID] = w
	}
	if w.count >= l.max {
		return false
	}
	w.count++
	return true
}
