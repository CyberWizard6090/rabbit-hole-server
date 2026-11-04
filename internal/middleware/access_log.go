package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	contextutil "rabbit-hole-server/internal/http/context"
)

func AccessLog(logger *slog.Logger, skipPaths ...string) gin.HandlerFunc {
	skip := make(map[string]struct{}, len(skipPaths))
	for _, p := range skipPaths {
		skip[p] = struct{}{}
	}

	return func(c *gin.Context) {
		if _, ok := skip[c.Request.URL.Path]; ok {
			c.Next()
			return
		}

		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}

		attrs := []any{
			"request_id", contextutil.GetRequestID(c),
			"method", c.Request.Method,
			"route", route,
			"status", status,
			"duration_ms", float64(time.Since(start).Microseconds()) / 1000,
			"bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
		}
		if uid, err := contextutil.GetUserID(c); err == nil {
			attrs = append(attrs, "user_id", uid)
		}

		level := slog.LevelInfo
		if status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		logger.Log(c.Request.Context(), level, "request", attrs...)
	}
}
