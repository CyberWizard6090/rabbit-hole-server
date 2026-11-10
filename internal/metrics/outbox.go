package metrics

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"rabbit-hole-server/internal/domain"
)

// QueueSource reads the queue state.
type QueueSource interface {
	QueueStats(ctx context.Context) ([]domain.OutboxQueueStat, error)
}

// RegisterOutbox exports the queue. The database is asked at most once per ttl however often it is scraped.
func (m *Metrics) RegisterOutbox(source QueueSource, ttl time.Duration) {
	m.registry.MustRegister(&queueCollector{
		source: source, ttl: ttl, now: time.Now,
		pending: prometheus.NewDesc("rabbit_hole_outbox_pending", "Events waiting to be handled (including retries).", []string{"event_type"}, nil),
		oldest:  prometheus.NewDesc("rabbit_hole_outbox_oldest_due_age_seconds", "Age of the longest-waiting event that is due now; 0 when the queue keeps up.", []string{"event_type"}, nil),
		dead:    prometheus.NewDesc("rabbit_hole_outbox_dead", "Events that exhausted their attempts and need a person.", []string{"event_type"}, nil),
		up:      prometheus.NewDesc("rabbit_hole_outbox_stats_up", "1 when the queue state could be read at the last scrape.", nil, nil),
	})
}

type queueCollector struct {
	source QueueSource
	ttl    time.Duration
	now    func() time.Time

	pending, oldest, dead, up *prometheus.Desc

	mu      sync.Mutex
	cached  []domain.OutboxQueueStat
	fetched time.Time
	healthy bool
}

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pending
	ch <- c.oldest
	ch <- c.dead
	ch <- c.up
}

func (c *queueCollector) stats() ([]domain.OutboxQueueStat, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fetched.IsZero() && c.now().Sub(c.fetched) < c.ttl {
		return c.cached, c.healthy
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stats, err := c.source.QueueStats(ctx)
	c.fetched = c.now()
	if err != nil {
		slog.Warn("metrics: cannot read the outbox queue", "error", err)
		c.healthy = false
		return c.cached, false
	}
	c.cached, c.healthy = stats, true
	return stats, true
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	stats, healthy := c.stats()
	up := 0.0
	if healthy {
		up = 1
	}
	ch <- prometheus.MustNewConstMetric(c.up, prometheus.GaugeValue, up)
	for _, s := range stats {
		ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, float64(s.Pending), s.EventType)
		ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, s.OldestDueSeconds, s.EventType)
		ch <- prometheus.MustNewConstMetric(c.dead, prometheus.GaugeValue, float64(s.Dead), s.EventType)
	}
}
