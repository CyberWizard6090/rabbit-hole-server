package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/http/response"
)

// RateLimiter returns a fixed-window, per-client-IP limiter. Every call creates
// an independent counter set, so attach separate instances to routes that need
// separate budgets (for example login and refresh).
func RateLimiter(max int, period time.Duration) gin.HandlerFunc {
	type client struct {
		count     int
		resetTime time.Time
	}

	var (
		mu        sync.Mutex
		clients   = make(map[string]*client)
		nextSweep = time.Now().Add(period)
	)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		mu.Lock()
		// Drop expired windows so the map cannot grow without bound.
		if now.After(nextSweep) {
			for key, cl := range clients {
				if now.After(cl.resetTime) {
					delete(clients, key)
				}
			}
			nextSweep = now.Add(period)
		}

		cl, ok := clients[ip]
		if !ok || now.After(cl.resetTime) {
			cl = &client{count: 0, resetTime: now.Add(period)}
			clients[ip] = cl
		}

		cl.count++
		exceeded := cl.count > max
		mu.Unlock()

		if exceeded {
			response.Error(c, http.StatusTooManyRequests, "too many requests")
			c.Abort()
			return
		}

		c.Next()
	}
}
