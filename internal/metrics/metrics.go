// Package metrics exposes what an operator needs to watch the server: request rate, errors and latency per
// route, the depth and age of the delivery queue, dead events, the database pool and live WebSocket connections.
package metrics

import (
	"crypto/subtle"
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics owns a registry of its own so tests and replicas never share state.
type Metrics struct {
	registry *prometheus.Registry

	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight prometheus.Gauge
	outbox   *prometheus.CounterVec
}

func New() *Metrics {
	m := &Metrics{
		registry: prometheus.NewRegistry(),
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rabbit_hole_http_requests_total", Help: "HTTP requests by method, route template and status code.",
		}, []string{"method", "route", "status"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "rabbit_hole_http_request_duration_seconds", Help: "HTTP request latency by method and route template.",
			Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
		}, []string{"method", "route"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "rabbit_hole_http_requests_in_flight", Help: "HTTP requests being served right now.",
		}),
		outbox: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "rabbit_hole_outbox_events_processed_total",
			Help: "Outbox events handled by this process: result is ok, retry (will be tried again) or dead (given up).",
		}, []string{"event_type", "result"}),
	}
	m.registry.MustRegister(
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		m.requests, m.duration, m.inFlight, m.outbox,
	)
	return m
}

// Middleware records every request. The route label is the registered template, never the raw path, so the
// number of series stays bounded however many ids clients ask for.
func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		if route == "/metrics" {
			c.Next()
			return
		}
		m.inFlight.Inc()
		start := time.Now()
		c.Next()
		m.inFlight.Dec()
		route = c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		m.requests.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		m.duration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}

// Handler serves the metrics. With a token it requires `Authorization: Bearer <token>`.
func (m *Metrics) Handler(token string) gin.HandlerFunc {
	h := promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
	return func(c *gin.Context) {
		if token != "" {
			given := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
			if !strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") || subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
				c.Header("WWW-Authenticate", "Bearer")
				c.AbortWithStatus(http.StatusUnauthorized)
				return
			}
		}
		h.ServeHTTP(c.Writer, c.Request)
	}
}

// ObserveOutbox implements service.OutboxObserver.
func (m *Metrics) ObserveOutbox(eventType, result string) {
	m.outbox.WithLabelValues(eventType, result).Inc()
}

// RegisterDB exports the connection pool of the database.
func (m *Metrics) RegisterDB(db *sql.DB) {
	m.registry.MustRegister(collectors.NewDBStatsCollector(db, "main"))
}

// RegisterConnections exports the number of open WebSocket connections.
func (m *Metrics) RegisterConnections(count func() int) {
	m.registry.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "rabbit_hole_websocket_connections", Help: "Open WebSocket connections on this process.",
	}, func() float64 { return float64(count()) }))
}
