package ratelimit

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type BackoffConfig struct {
	Threshold int
	Base      time.Duration
	Max       time.Duration
	Forget    time.Duration
	MaxKeys   int
}

type backoffEntry struct {
	failures  int
	lockedTil time.Time
	lastFail  time.Time
}

type Backoff struct {
	mu      sync.Mutex
	cfg     BackoffConfig
	now     func() time.Time
	entries map[string]*backoffEntry
}

func NewBackoff(cfg BackoffConfig) *Backoff {
	if cfg.MaxKeys <= 0 {
		cfg.MaxKeys = DefaultMaxKeys
	}
	if cfg.Forget <= 0 {
		cfg.Forget = time.Hour
	}
	return &Backoff{cfg: cfg, now: time.Now, entries: make(map[string]*backoffEntry)}
}

func (b *Backoff) Enabled() bool {
	return b != nil && b.cfg.Threshold > 0
}

func backoffKey(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:16])
}

func (b *Backoff) Locked(subject string) (bool, time.Duration) {
	if !b.Enabled() {
		return false, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.entries[backoffKey(subject)]
	if !ok {
		return false, 0
	}
	now := b.now()
	if now.Before(entry.lockedTil) {
		return true, entry.lockedTil.Sub(now)
	}
	return false, 0
}

func (b *Backoff) Fail(subject string) time.Duration {
	if !b.Enabled() {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	key := backoffKey(subject)
	entry, ok := b.entries[key]
	if ok && now.Sub(entry.lastFail) > b.cfg.Forget {
		entry = nil
		ok = false
	}
	if !ok {
		if len(b.entries) >= b.cfg.MaxKeys {
			b.makeRoomLocked(now)
		}
		entry = &backoffEntry{}
		b.entries[key] = entry
	}
	entry.failures++
	entry.lastFail = now
	if entry.failures < b.cfg.Threshold {
		return 0
	}
	delay := b.delay(entry.failures - b.cfg.Threshold)
	entry.lockedTil = now.Add(delay)
	return delay
}

func (b *Backoff) delay(step int) time.Duration {
	delay := b.cfg.Base
	for i := 0; i < step && delay < b.cfg.Max; i++ {
		delay *= 2
	}
	if b.cfg.Max > 0 && delay > b.cfg.Max {
		delay = b.cfg.Max
	}
	return delay
}

func (b *Backoff) Succeed(subject string) {
	if !b.Enabled() {
		return
	}
	b.mu.Lock()
	delete(b.entries, backoffKey(subject))
	b.mu.Unlock()
}

func (b *Backoff) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.entries)
}

func (b *Backoff) makeRoomLocked(now time.Time) {
	for key, entry := range b.entries {
		if now.Sub(entry.lastFail) > b.cfg.Forget {
			delete(b.entries, key)
		}
	}
	if len(b.entries) < b.cfg.MaxKeys {
		return
	}
	for key, entry := range b.entries {
		if !now.Before(entry.lockedTil) {
			delete(b.entries, key)
			return
		}
	}
	for key := range b.entries {
		delete(b.entries, key)
		return
	}
}
