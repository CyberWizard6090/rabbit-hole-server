package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// DefaultMaxBodyBytes is the request body cap applied to the whole API.
const DefaultMaxBodyBytes int64 = 1 << 20 // 1 MiB

// BodyLimit caps the request body size. Reads past the limit fail, which makes
// JSON binding return an error instead of buffering an arbitrarily large body.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func BodyLimitByRoute(defaultMax int64, overrides map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := defaultMax
		if override, ok := overrides[c.Request.Method+" "+c.FullPath()]; ok {
			limit = override
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		}
		c.Next()
	}
}
