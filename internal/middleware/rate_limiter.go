package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

func RateLimiter(max int, period time.Duration) gin.HandlerFunc {
	type client struct {
		count     int
		resetTime time.Time
	}

	var (
		mu      sync.Mutex
		clients = make(map[string]*client)
	)

	return func(c *gin.Context) {
		ip := c.ClientIP()

		mu.Lock()
		cl, ok := clients[ip]
		now := time.Now()

		if !ok || now.After(cl.resetTime) {
			cl = &client{count: 0, resetTime: now.Add(period)}
			clients[ip] = cl
		}

		cl.count++
		exceeded := cl.count > max
		mu.Unlock()

		if exceeded {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}

		c.Next()
	}
}
