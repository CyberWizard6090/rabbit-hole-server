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

type OutboxRepository interface {
	Add(ctx context.Context, events []OutboxEvent) error
	Claim(ctx context.Context, types []string, limit int, lease time.Duration) ([]OutboxEvent, error)
	Complete(ctx context.Context, id uint) error
	Retry(ctx context.Context, id uint, reason string, at time.Time) error
	Bury(ctx context.Context, id uint, reason string) error
	HasDue(ctx context.Context, types []string) (bool, error)
}
