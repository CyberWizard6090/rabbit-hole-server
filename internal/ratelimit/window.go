package ratelimit

import (
	"net/netip"
	"sync"
	"time"
)

const DefaultMaxKeys = 100000

type windowEntry struct {
	count   int
	resetAt time.Time
}

type Window struct {
	mu        sync.Mutex
	max       int
	period    time.Duration
	maxKeys   int
	now       func() time.Time
	entries   map[string]*windowEntry
	nextSweep time.Time
}

func NewWindow(max int, period time.Duration) *Window {
	return &Window{
		max:     max,
		period:  period,
		maxKeys: DefaultMaxKeys,
		now:     time.Now,
		entries: make(map[string]*windowEntry),
	}
}

func (w *Window) WithMaxKeys(limit int) *Window {
	if limit > 0 {
		w.maxKeys = limit
	}
	return w
}

func (w *Window) WithClock(now func() time.Time) *Window {
	w.now = now
	return w
}

func (w *Window) Allow(key string) (bool, time.Duration) {
	if w == nil || w.max <= 0 {
		return true, 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	now := w.now()
	if w.nextSweep.IsZero() {
		w.nextSweep = now.Add(w.period)
	}
	if now.After(w.nextSweep) {
		w.sweepLocked(now)
	}

	entry, ok := w.entries[key]
	if !ok || now.After(entry.resetAt) {
		if !ok && len(w.entries) >= w.maxKeys {
			w.sweepLocked(now)
			if len(w.entries) >= w.maxKeys {
				w.evictLocked()
			}
		}
		entry = &windowEntry{resetAt: now.Add(w.period)}
		w.entries[key] = entry
	}

	if entry.count >= w.max {
		return false, entry.resetAt.Sub(now)
	}
	entry.count++
	return true, 0
}

func (w *Window) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.entries)
}

func (w *Window) sweepLocked(now time.Time) {
	for key, entry := range w.entries {
		if now.After(entry.resetAt) {
			delete(w.entries, key)
		}
	}
	w.nextSweep = now.Add(w.period)
}

func (w *Window) evictLocked() {
	var oldestKey string
	var oldest time.Time
	scanned := 0
	for key, entry := range w.entries {
		if scanned == 0 || entry.resetAt.Before(oldest) {
			oldestKey, oldest = key, entry.resetAt
		}
		scanned++
		if scanned >= evictionSample {
			break
		}
	}
	delete(w.entries, oldestKey)
}

const evictionSample = 16

func ClientKey(address string) string {
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return address
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return ip.String()
	}
	prefix, err := ip.Prefix(64)
	if err != nil {
		return ip.String()
	}
	return prefix.String()
}
