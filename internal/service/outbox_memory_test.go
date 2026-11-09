package service

import (
	"context"
	"sort"
	"sync"
	"time"

	"rabbit-hole-server/internal/domain"
)

type memOutboxRow struct {
	event       domain.OutboxEvent
	done        bool
	dead        bool
	nextAttempt time.Time
	lockedUntil time.Time
	lastError   string
}

type memOutbox struct {
	mu     sync.Mutex
	rows   []*memOutboxRow
	nextID uint
	now    func() time.Time
}

func newMemOutbox() *memOutbox { return &memOutbox{now: time.Now} }

func (m *memOutbox) Add(_ context.Context, events []domain.OutboxEvent) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, e := range events {
		m.nextID++
		e.ID = m.nextID
		e.CreatedAt = m.now()
		m.rows = append(m.rows, &memOutboxRow{event: e, nextAttempt: m.now()})
	}
	return nil
}

func (m *memOutbox) due(row *memOutboxRow, types []string) bool {
	if row.done || row.dead || row.nextAttempt.After(m.now()) || row.lockedUntil.After(m.now()) {
		return false
	}
	for _, t := range types {
		if t == row.event.Type {
			return true
		}
	}
	return false
}

func (m *memOutbox) Claim(_ context.Context, types []string, limit int, lease time.Duration) ([]domain.OutboxEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []domain.OutboxEvent
	for _, row := range m.rows {
		if len(out) >= limit {
			break
		}
		if !m.due(row, types) {
			continue
		}
		row.event.Attempts++
		row.lockedUntil = m.now().Add(lease)
		out = append(out, row.event)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memOutbox) find(id uint) *memOutboxRow {
	for _, row := range m.rows {
		if row.event.ID == id {
			return row
		}
	}
	return nil
}

func (m *memOutbox) Complete(_ context.Context, id uint) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row := m.find(id); row != nil {
		row.done = true
		row.lockedUntil = time.Time{}
	}
	return nil
}

func (m *memOutbox) Retry(_ context.Context, id uint, reason string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row := m.find(id); row != nil {
		row.nextAttempt, row.lastError, row.lockedUntil = at, reason, time.Time{}
	}
	return nil
}

func (m *memOutbox) Bury(_ context.Context, id uint, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row := m.find(id); row != nil {
		row.dead, row.lastError, row.lockedUntil = true, reason, time.Time{}
	}
	return nil
}

func (m *memOutbox) HasDue(_ context.Context, types []string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if m.due(row, types) {
			return true, nil
		}
	}
	return false, nil
}

func (m *memOutbox) rowsOfType(eventType string) []memOutboxRow {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []memOutboxRow
	for _, row := range m.rows {
		if row.event.Type == eventType {
			out = append(out, *row)
		}
	}
	return out
}

type passthroughTx struct{}

func (passthroughTx) Run(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
