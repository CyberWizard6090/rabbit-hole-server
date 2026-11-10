package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
)

func scrape(t *testing.T, h http.Handler, authorization string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func newEngine(m *Metrics, token string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(m.Middleware())
	r.GET("/metrics", m.Handler(token))
	r.GET("/items/:id", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/boom", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	return r
}

func TestRequestsAreCountedPerRouteTemplate(t *testing.T) {
	m := New()
	r := newEngine(m, "")
	for _, path := range []string{"/items/1", "/items/2", "/items/99999", "/boom", "/nowhere"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
	}
	_, body := scrape(t, r, "")
	for _, want := range []string{
		`rabbit_hole_http_requests_total{method="GET",route="/items/:id",status="200"} 3`,
		`rabbit_hole_http_requests_total{method="GET",route="/boom",status="500"} 1`,
		`rabbit_hole_http_requests_total{method="GET",route="unmatched",status="404"} 1`,
		`rabbit_hole_http_request_duration_seconds_count{method="GET",route="/items/:id"} 3`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
	if strings.Contains(body, `/items/1"`) || strings.Contains(body, "/nowhere") {
		t.Fatal("raw paths leaked into labels: the number of series would grow without bound")
	}
	if strings.Contains(body, `route="/metrics"`) {
		t.Fatal("scrapes must not count themselves")
	}
}

func TestTheEndpointNeedsTheTokenWhenOneIsSet(t *testing.T) {
	r := newEngine(New(), "s3cr3t-token-value")
	for _, tc := range []struct {
		name, header string
		want         int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong token", "Bearer nope", http.StatusUnauthorized},
		{"basic scheme", "Basic s3cr3t-token-value", http.StatusUnauthorized},
		{"right token", "Bearer s3cr3t-token-value", http.StatusOK},
	} {
		if code, _ := scrape(t, r, tc.header); code != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, code, tc.want)
		}
	}
}

type fakeQueue struct {
	calls int
	stats []domain.OutboxQueueStat
	err   error
}

func (f *fakeQueue) QueueStats(context.Context) ([]domain.OutboxQueueStat, error) {
	f.calls++
	return f.stats, f.err
}

func TestQueueStateIsExportedAndCachedBetweenScrapes(t *testing.T) {
	m := New()
	q := &fakeQueue{stats: []domain.OutboxQueueStat{{EventType: "delivery.mail", Pending: 7, OldestDueSeconds: 42, Dead: 2}}}
	m.RegisterOutbox(q, time.Hour)
	r := newEngine(m, "")

	_, body := scrape(t, r, "")
	for _, want := range []string{
		`rabbit_hole_outbox_pending{event_type="delivery.mail"} 7`,
		`rabbit_hole_outbox_oldest_due_age_seconds{event_type="delivery.mail"} 42`,
		`rabbit_hole_outbox_dead{event_type="delivery.mail"} 2`,
		`rabbit_hole_outbox_stats_up 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
	scrape(t, r, "")
	scrape(t, r, "")
	if q.calls != 1 {
		t.Fatalf("the database was asked %d times for 3 scrapes, want 1", q.calls)
	}
}

func TestAFailingQueueReadIsVisible(t *testing.T) {
	m := New()
	m.RegisterOutbox(&fakeQueue{err: errors.New("db down")}, time.Millisecond)
	_, body := scrape(t, newEngine(m, ""), "")
	if !strings.Contains(body, "rabbit_hole_outbox_stats_up 0") {
		t.Fatal("a failed queue read must show up as stats_up 0 so the alerts are not silently blind")
	}
}

func TestOutboxOutcomesAreCounted(t *testing.T) {
	m := New()
	m.ObserveOutbox("delivery.mail", "ok")
	m.ObserveOutbox("delivery.mail", "ok")
	m.ObserveOutbox("delivery.mail", "dead")
	_, body := scrape(t, newEngine(m, ""), "")
	for _, want := range []string{
		`rabbit_hole_outbox_events_processed_total{event_type="delivery.mail",result="ok"} 2`,
		`rabbit_hole_outbox_events_processed_total{event_type="delivery.mail",result="dead"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
}

func TestConnectionsGaugeFollowsTheSource(t *testing.T) {
	m := New()
	open := 3
	m.RegisterConnections(func() int { return open })
	r := newEngine(m, "")
	if _, body := scrape(t, r, ""); !strings.Contains(body, "rabbit_hole_websocket_connections 3") {
		t.Fatal("gauge lacks the current value")
	}
	open = 5
	if _, body := scrape(t, r, ""); !strings.Contains(body, "rabbit_hole_websocket_connections 5") {
		t.Fatal("gauge did not follow the source")
	}
}

func TestAPanickingRequestIsCountedAsAServerError(t *testing.T) {
	m := New()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(gin.Recovery(), m.Middleware())
	r.GET("/metrics", m.Handler(""))
	r.GET("/panic", func(*gin.Context) { panic("boom") })

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("the recovery middleware must still answer 500, got %d", rec.Code)
	}
	_, body := scrape(t, r, "")
	if !strings.Contains(body, `rabbit_hole_http_requests_total{method="GET",route="/panic",status="500"} 1`) {
		t.Errorf("the panic is not counted as a 500:\n%.600s", body)
	}
	if !strings.Contains(body, "rabbit_hole_http_requests_in_flight 0") {
		t.Error("a panic left the in-flight gauge raised")
	}
}

func TestUnknownMethodsShareOneLabel(t *testing.T) {
	m := New()
	r := newEngine(m, "")
	for _, method := range []string{"FOO", "BAR1", "PROPFIND", "ZZZ"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/anything", nil))
	}
	_, body := scrape(t, r, "")
	if !strings.Contains(body, `rabbit_hole_http_requests_total{method="other",route="unmatched",status="404"} 4`) {
		t.Errorf("unknown methods are not folded into one series:\n%.800s", body)
	}
	for _, method := range []string{"FOO", "BAR1", "ZZZ"} {
		if strings.Contains(body, `method="`+method+`"`) {
			t.Fatalf("method %s became a label value: a client could create unlimited series", method)
		}
	}
}
