package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/ratelimit"
)

func TestRateLimiterKeysIPv6ByPrefixAndIPv4ByAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(RateLimiter(ratelimit.NewMemory(), "test", 1, time.Hour))
	router.GET("/limited", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	status := func(remote string) int {
		req := httptest.NewRequest(http.MethodGet, "/limited", nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec.Code
	}

	if got := status("[2001:db8:1:2::1]:4000"); got != http.StatusNoContent {
		t.Fatalf("first IPv6 request = %d", got)
	}
	if got := status("[2001:db8:1:2:ffff:ffff:ffff:ffff]:4001"); got != http.StatusTooManyRequests {
		t.Fatalf("another address of the same /64 must share the budget, got %d", got)
	}
	if got := status("[2001:db8:1:3::1]:4000"); got != http.StatusNoContent {
		t.Fatalf("a different /64 has its own budget, got %d", got)
	}
	if got := status("203.0.113.1:5000"); got != http.StatusNoContent {
		t.Fatalf("first IPv4 request = %d", got)
	}
	if got := status("203.0.113.2:5000"); got != http.StatusNoContent {
		t.Fatalf("IPv4 addresses are limited individually, got %d", got)
	}
	if got := status("203.0.113.1:5001"); got != http.StatusTooManyRequests {
		t.Fatalf("second request of one IPv4 address = %d", got)
	}
}
