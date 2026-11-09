package middleware

import (
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/ratelimit"
)

func RateLimiter(limiter ratelimit.Limiter, bucket string, max int, period time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, wait, err := limiter.Allow(c.Request.Context(), bucket, ratelimit.ClientKey(c.ClientIP()), max, period)
		if err != nil {
			slog.Warn("rate limit: store unavailable, letting the request through", "bucket", bucket, "error", err)
			c.Next()
			return
		}
		if allowed {
			c.Next()
			return
		}
		retryAfter := int(math.Ceil(wait.Seconds()))
		if retryAfter < 1 {
			retryAfter = 1
		}
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		response.Fail(c, http.StatusTooManyRequests, "RATE_LIMIT_EXCEEDED", map[string]any{"retry_after": retryAfter})
		c.Abort()
	}
}
