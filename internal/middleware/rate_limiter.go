package middleware

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/ratelimit"
)

func RateLimiter(max int, period time.Duration) gin.HandlerFunc {
	window := ratelimit.NewWindow(max, period)

	return func(c *gin.Context) {
		allowed, wait := window.Allow(ratelimit.ClientKey(c.ClientIP()))
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
