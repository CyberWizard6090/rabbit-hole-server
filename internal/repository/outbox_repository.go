package repository

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type outboxRepository struct {
	db *gorm.DB
}

func NewOutboxRepository(db *gorm.DB) domain.OutboxRepository {
	return &outboxRepository{db: db}
}

func (r *outboxRepository) Add(ctx context.Context, events []domain.OutboxEvent) error {
	if len(events) == 0 {
		return nil
	}
	db := database.DBFromContext(ctx, r.db)
	for _, e := range events {
		if err := db.Exec(
			`INSERT INTO outbox_events (event_type, aggregate_type, aggregate_id, payload) VALUES (?, ?, ?, ?::jsonb)`,
			e.Type, e.AggregateType, e.AggregateID, string(e.Payload),
		).Error; err != nil {
			return mapErr(err)
		}
	}
	return nil
}

type outboxRow struct {
	ID            uint
	EventType     string
	AggregateType string
	AggregateID   uint
	Payload       []byte
	Attempts      int
	CreatedAt     time.Time
}

func (r *outboxRepository) Claim(
	ctx context.Context, types []string, limit int, lease time.Duration,
) ([]domain.OutboxEvent, error) {
	if limit <= 0 || len(types) == 0 {
		return nil, nil
	}
	var rows []outboxRow
	err := database.DBFromContext(ctx, r.db).Raw(`
		UPDATE outbox_events SET locked_until = now() + make_interval(secs => ?), attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM outbox_events
			WHERE published_at IS NULL AND dead_at IS NULL AND event_type IN ?
			  AND next_attempt_at <= now() AND (locked_until IS NULL OR locked_until < now())
			ORDER BY id LIMIT ? FOR UPDATE SKIP LOCKED
		)
		RETURNING id, event_type, aggregate_type, aggregate_id, payload, attempts, created_at`,
		lease.Seconds(), types, limit,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	out := make([]domain.OutboxEvent, len(rows))
	for i, row := range rows {
		out[i] = domain.OutboxEvent{
			ID: row.ID, Type: row.EventType, AggregateType: row.AggregateType, AggregateID: row.AggregateID,
			Payload: row.Payload, Attempts: row.Attempts, CreatedAt: row.CreatedAt,
		}
	}
	return out, nil
}

func (r *outboxRepository) Complete(ctx context.Context, id uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		`UPDATE outbox_events SET published_at = now(), locked_until = NULL, last_error = NULL,
			payload = CASE WHEN event_type LIKE 'delivery.%' THEN '{}'::jsonb ELSE payload END WHERE id = ?`, id,
	).Error)
}

func (r *outboxRepository) Retry(ctx context.Context, id uint, reason string, at time.Time) error {
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		`UPDATE outbox_events SET next_attempt_at = ?, locked_until = NULL, last_error = ? WHERE id = ?`, at, reason, id,
	).Error)
}

func (r *outboxRepository) Bury(ctx context.Context, id uint, reason string) error {
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		`UPDATE outbox_events SET dead_at = now(), locked_until = NULL, last_error = ? WHERE id = ?`, reason, id,
	).Error)
}

func (r *outboxRepository) HasDue(ctx context.Context, types []string) (bool, error) {
	if len(types) == 0 {
		return false, nil
	}
	var found bool
	err := database.DBFromContext(ctx, r.db).Raw(`
		SELECT EXISTS (
			SELECT 1 FROM outbox_events
			WHERE published_at IS NULL AND dead_at IS NULL AND event_type IN ?
			  AND next_attempt_at <= now() AND (locked_until IS NULL OR locked_until < now())
		)`, types).Scan(&found).Error
	return found, mapErr(err)
}

// QueueStats summarizes the queue per event type for the metrics: how many events wait, how long the longest
// waiting due one has waited, and how many are dead. Both partial indexes of the table serve it.
func (r *outboxRepository) QueueStats(ctx context.Context) ([]domain.OutboxQueueStat, error) {
	var rows []struct {
		EventType string
		Pending   int64
		Oldest    float64
		Dead      int64
	}
	err := database.DBFromContext(ctx, r.db).Raw(`
		SELECT event_type,
			count(*) FILTER (WHERE published_at IS NULL AND dead_at IS NULL) AS pending,
			COALESCE(EXTRACT(EPOCH FROM now() - min(next_attempt_at) FILTER (
				WHERE published_at IS NULL AND dead_at IS NULL AND next_attempt_at <= now()
				  AND (locked_until IS NULL OR locked_until < now()))), 0)::float8 AS oldest,
			count(*) FILTER (WHERE dead_at IS NOT NULL) AS dead
		FROM outbox_events
		WHERE (published_at IS NULL AND dead_at IS NULL) OR dead_at IS NOT NULL
		GROUP BY event_type`).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.OutboxQueueStat, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.OutboxQueueStat{EventType: row.EventType, Pending: row.Pending, OldestDueSeconds: row.Oldest, Dead: row.Dead})
	}
	return out, nil
}
