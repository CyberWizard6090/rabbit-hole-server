package ratelimit

import (
	"context"
	"strconv"
	"sync"
	"time"
)

type Limiter interface {
	Allow(ctx context.Context, bucket, key string, max int, period time.Duration) (bool, time.Duration, error)
}

type Memory struct {
	mu      sync.Mutex
	windows map[string]*Window
	now     func() time.Time
}

func NewMemory() *Memory {
	return &Memory{windows: make(map[string]*Window)}
}

func (m *Memory) WithClock(now func() time.Time) *Memory {
	m.now = now
	return m
}

func (m *Memory) Allow(_ context.Context, bucket, key string, max int, period time.Duration) (bool, time.Duration, error) {
	id := bucket + "|" + strconv.Itoa(max) + "|" + period.String()
	m.mu.Lock()
	window, ok := m.windows[id]
	if !ok {
		window = NewWindow(max, period)
		if m.now != nil {
			window.WithClock(m.now)
		}
		m.windows[id] = window
	}
	m.mu.Unlock()
	allowed, wait := window.Allow(key)
	return allowed, wait, nil
}

type Lockout interface {
	Enabled() bool
	Locked(ctx context.Context, subject string) (bool, time.Duration, error)
	Fail(ctx context.Context, subject string) (time.Duration, error)
	Succeed(ctx context.Context, subject string) error
}

type MemoryLockout struct {
	backoff *Backoff
}

func NewMemoryLockout(cfg BackoffConfig) *MemoryLockout {
	return &MemoryLockout{backoff: NewBackoff(cfg)}
}

func (m *MemoryLockout) Enabled() bool { return m != nil && m.backoff.Enabled() }

func (m *MemoryLockout) Locked(_ context.Context, subject string) (bool, time.Duration, error) {
	locked, wait := m.backoff.Locked(subject)
	return locked, wait, nil
}

func (m *MemoryLockout) Fail(_ context.Context, subject string) (time.Duration, error) {
	return m.backoff.Fail(subject), nil
}

func (m *MemoryLockout) Succeed(_ context.Context, subject string) error {
	m.backoff.Succeed(subject)
	return nil
}

func LockDelay(cfg BackoffConfig, failures int) time.Duration {
	if cfg.Threshold <= 0 || failures < cfg.Threshold {
		return 0
	}
	delay := cfg.Base
	for i := 0; i < failures-cfg.Threshold && delay < cfg.Max; i++ {
		delay *= 2
	}
	if cfg.Max > 0 && delay > cfg.Max {
		delay = cfg.Max
	}
	return delay
}

func SubjectKey(subject string) string {
	return backoffKey(subject)
}
