package middleware

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func jsonLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line is not JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func newLoggedRouter(buf *bytes.Buffer) *gin.Engine {
	gin.SetMode(gin.TestMode)
	logger := jsonLogger(buf)
	r := gin.New()
	r.Use(RequestID(), AccessLog(logger, "/healthz"), Recovery(logger))
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/items/:id", func(c *gin.Context) {
		c.Set("userID", uint(7))
		c.String(http.StatusOK, "ok")
	})
	r.GET("/boom", func(c *gin.Context) { c.Status(http.StatusInternalServerError) })
	r.GET("/panic", func(c *gin.Context) { panic("secret internal detail") })
	return r
}

func TestRequestIDGeneratedWhenMissingOrUnsafe(t *testing.T) {
	var buf bytes.Buffer
	r := newLoggedRouter(&buf)

	for name, header := range map[string]string{"missing": "", "too short": "abc", "unsafe": "bad id\nwith newline!!"} {
		req := httptest.NewRequest(http.MethodGet, "/items/1", nil)
		if header != "" {
			req.Header.Set("X-Request-ID", header)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		got := rec.Header().Get("X-Request-ID")
		if !validRequestID.MatchString(got) || got == header {
			t.Errorf("%s: X-Request-ID = %q, want a freshly generated safe ID", name, got)
		}
	}
}

func TestRequestIDHonoursSafeClientValue(t *testing.T) {
	var buf bytes.Buffer
	r := newLoggedRouter(&buf)
	req := httptest.NewRequest(http.MethodGet, "/items/1", nil)
	req.Header.Set("X-Request-ID", "client-trace-12345")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "client-trace-12345" {
		t.Fatalf("X-Request-ID = %q, want the client's value echoed", got)
	}
}

func TestAccessLogFieldsAndLevels(t *testing.T) {
	var buf bytes.Buffer
	r := newLoggedRouter(&buf)

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/items/42?cursor=SECRET", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/boom", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/healthz", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/nope", nil))

	lines := logLines(t, &buf)
	if len(lines) != 3 {
		t.Fatalf("got %d log lines, want 3 (health check skipped): %s", len(lines), buf.String())
	}

	ok := lines[0]
	if ok["route"] != "/items/:id" || ok["status"] != float64(200) || ok["method"] != "GET" || ok["level"] != "INFO" {
		t.Errorf("unexpected access log: %v", ok)
	}
	if ok["user_id"] != float64(7) || ok["request_id"] == "" || ok["request_id"] == nil {
		t.Errorf("missing user_id/request_id: %v", ok)
	}
	if strings.Contains(buf.String(), "SECRET") || strings.Contains(buf.String(), "/items/42") {
		t.Errorf("raw URL/query leaked into logs: %s", buf.String())
	}
	if lines[1]["level"] != "ERROR" || lines[1]["status"] != float64(500) {
		t.Errorf("5xx must be logged at ERROR: %v", lines[1])
	}
	if lines[2]["route"] != "unmatched" || lines[2]["level"] != "INFO" {
		t.Errorf("404 should be logged as INFO with route 'unmatched': %v", lines[2])
	}
}

func TestRecoveryReturns500WithoutLeakingPanicAndLogsIt(t *testing.T) {
	var buf bytes.Buffer
	r := newLoggedRouter(&buf)
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set("X-Request-ID", "trace-for-panic-1")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "secret internal detail") || !strings.Contains(rec.Body.String(), "INTERNAL_ERROR") {
		t.Fatalf("response must use the error envelope without the panic value: %s", rec.Body.String())
	}

	var recovered map[string]any
	for _, line := range logLines(t, &buf) {
		if line["msg"] == "panic recovered" {
			recovered = line
		}
	}
	if recovered == nil || recovered["request_id"] != "trace-for-panic-1" ||
		!strings.Contains(recovered["panic"].(string), "secret internal detail") || recovered["stack"] == "" {
		t.Fatalf("panic not logged with request ID and stack: %v", recovered)
	}
}
