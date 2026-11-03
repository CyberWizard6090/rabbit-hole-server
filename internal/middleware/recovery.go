package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	contextutil "rabbit-hole-server/internal/http/context"
	"rabbit-hole-server/internal/http/response"
)

func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		logger.Error("panic recovered",
			"request_id", contextutil.GetRequestID(c),
			"method", c.Request.Method,
			"route", c.FullPath(),
			"panic", recovered,
			"stack", string(debug.Stack()),
		)
		response.Internal(c)
		c.AbortWithStatus(http.StatusInternalServerError)
	})
}
