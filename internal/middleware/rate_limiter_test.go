package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/ratelimit"
)

func TestRateLimiterReturns429AndStopsChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RateLimiter(ratelimit.NewMemory(), "test", 1, time.Hour))

	handlerCalls := 0
	router.GET("/limited", func(c *gin.Context) {
		handlerCalls++
		c.Status(http.StatusOK)
	})

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/limited", nil))
	if first.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want %d", first.Code, http.StatusOK)
	}

	second := httptest.NewRecorder()
	router.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/limited", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second request status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	if handlerCalls != 1 {
		t.Fatalf("downstream handler calls = %d, want 1", handlerCalls)
	}
	if second.Body.String() == "" {
		t.Fatal("429 response body is empty")
	}
	var body struct {
		Error struct {
			Code   string         `json:"code"`
			Params map[string]any `json:"params"`
		} `json:"error"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	retryAfter, _ := body.Error.Params["retry_after"].(float64)
	if body.Error.Code != "RATE_LIMIT_EXCEEDED" || retryAfter < 1 || retryAfter > 3600 {
		t.Fatalf("body = %s", second.Body.String())
	}
	if second.Header().Get("Retry-After") != strconv.Itoa(int(retryAfter)) {
		t.Fatalf("Retry-After = %q, params = %v", second.Header().Get("Retry-After"), body.Error.Params)
	}
}

func TestRateLimiterAllowsRequestsAfterPeriod(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RateLimiter(ratelimit.NewMemory(), "test", 1, 10*time.Millisecond))
	router.GET("/limited", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	first := httptest.NewRecorder()
	router.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/limited", nil))
	if first.Code != http.StatusNoContent {
		t.Fatalf("first status=%d", first.Code)
	}

	blocked := httptest.NewRecorder()
	router.ServeHTTP(blocked, httptest.NewRequest(http.MethodGet, "/limited", nil))
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("blocked status=%d", blocked.Code)
	}

	time.Sleep(15 * time.Millisecond)
	allowed := httptest.NewRecorder()
	router.ServeHTTP(allowed, httptest.NewRequest(http.MethodGet, "/limited", nil))
	if allowed.Code != http.StatusNoContent {
		t.Fatalf("allowed status=%d", allowed.Code)
	}
}
