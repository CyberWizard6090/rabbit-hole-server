package domain

import (
	"context"
	"encoding/json"
	"time"
)

const (
	OutboxNotifyEntry     = "notify.entry"
	OutboxDeliveryMail    = "delivery.mail"
	OutboxDeliveryWebhook = "delivery.webhook"
)

type OutboxEvent struct {
	ID            uint
	Type          string
	AggregateType string
	AggregateID   uint
	Payload       json.RawMessage
	Attempts      int
	CreatedAt     time.Time
}

// OutboxQueueStat is the state of the delivery queue for one event type, read for the metrics.
type OutboxQueueStat struct {
	EventType string
	// Pending counts events waiting to be handled, retries included.
	Pending int64
	// OldestDueSeconds is how long the longest-waiting due event has waited; 0 when none is due.
	OldestDueSeconds float64
	// Dead counts events that were given up on and are not removed yet.
	Dead int64
}

type OutboxRepository interface {
	QueueStats(ctx context.Context) ([]OutboxQueueStat, error)
	Add(ctx context.Context, events []OutboxEvent) error
	Claim(ctx context.Context, types []string, limit int, lease time.Duration) ([]OutboxEvent, error)
	Complete(ctx context.Context, id uint) error
	Retry(ctx context.Context, id uint, reason string, at time.Time) error
	Bury(ctx context.Context, id uint, reason string) error
	HasDue(ctx context.Context, types []string) (bool, error)
}
