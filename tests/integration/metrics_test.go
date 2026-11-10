package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"rabbit-hole-server/internal/config"
)

func scrapeMetrics(t *testing.T, tc *TestContext, authorization string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	tc.Raw.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestMetrics_RequestsAreCountedByRouteTemplate(t *testing.T) {
	tc := newTestContext(t)
	_, token := newUser(t, tc, fmt.Sprintf("metrics-%d@example.test", time.Now().UnixNano()))
	for _, id := range []string{"1", "2", "987654"} {
		request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+id, token, nil)
	}
	code, body := scrapeMetrics(t, tc, "")
	if code != http.StatusOK {
		t.Fatalf("scrape: %d", code)
	}
	if !strings.Contains(body, `route="/api/v1/tasks/:task_id"`) {
		t.Fatalf("no series for the task route template:\n%.800s", body)
	}
	for _, id := range []string{"/tasks/1\"", "/tasks/987654"} {
		if strings.Contains(body, id) {
			t.Fatalf("a concrete id leaked into a label (%s): the cardinality would be unbounded", id)
		}
	}
	if !strings.Contains(body, `route="/api/v1/auth/login"`) {
		t.Fatal("the sign-in made by the test is not counted")
	}
}

func TestMetrics_QueueDatabasePoolAndWorkersAreExported(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	f.comment(t, f.owner.token, f.taskID, "please look", f.memberA)

	_, body := scrapeMetrics(t, tc, "")
	for _, want := range []string{
		"rabbit_hole_outbox_stats_up 1",
		`rabbit_hole_outbox_events_processed_total{event_type="notify.entry",result="ok"}`,
		`go_sql_max_open_connections{db_name="main"}`,
		"rabbit_hole_websocket_connections 0",
		"rabbit_hole_http_requests_in_flight",
		"go_goroutines",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics lack %q", want)
		}
	}
}

func TestMetrics_AnUndeliverableEventShowsUpAsDeadAndBacklog(t *testing.T) {
	tc := newTestContext(t)
	insert := func(extra string) {
		t.Helper()
		if err := tc.DB.Exec(`INSERT INTO outbox_events (event_type, aggregate_type, aggregate_id, payload` + extra).Error; err != nil {
			t.Fatal(err)
		}
	}
	insert(`, next_attempt_at) VALUES ('delivery.mail', 'user', 1, '{}', now() - interval '10 minutes')`)
	insert(`, dead_at, last_error) VALUES ('delivery.webhook', 'user', 1, '{}', now(), 'boom')`)
	insert(`, published_at) VALUES ('delivery.mail', 'user', 1, '{}', now())`)

	_, body := scrapeMetrics(t, tc, "")
	if !strings.Contains(body, `rabbit_hole_outbox_pending{event_type="delivery.mail"} 1`) {
		t.Errorf("pending mail not exported (a processed event must not count):\n%.1500s", body)
	}
	if !strings.Contains(body, `rabbit_hole_outbox_dead{event_type="delivery.webhook"} 1`) {
		t.Error("the dead webhook event is not exported")
	}
	var age float64
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, `rabbit_hole_outbox_oldest_due_age_seconds{event_type="delivery.mail"}`) {
			fmt.Sscanf(line[strings.LastIndex(line, " ")+1:], "%g", &age)
		}
	}
	if age < 500 || age > 700 {
		t.Errorf("oldest due age = %v seconds, want about 600", age)
	}
}

func TestMetrics_TokenProtectsTheEndpoint(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Metrics.Token = "integration-metrics-token" })
	if code, _ := scrapeMetrics(t, tc, ""); code != http.StatusUnauthorized {
		t.Fatalf("anonymous scrape: %d, want 401", code)
	}
	if code, _ := scrapeMetrics(t, tc, "Bearer wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong token: %d, want 401", code)
	}
	if code, body := scrapeMetrics(t, tc, "Bearer integration-metrics-token"); code != http.StatusOK || !strings.Contains(body, "go_goroutines") {
		t.Fatalf("right token: %d", code)
	}
}

func TestMetrics_CanBeSwitchedOff(t *testing.T) {
	tc := newTestContextWith(t, func(cfg *config.Config) { cfg.Metrics.Enabled = false })
	if code, _ := scrapeMetrics(t, tc, ""); code != http.StatusNotFound {
		t.Fatalf("disabled metrics answer %d, want 404", code)
	}
}
