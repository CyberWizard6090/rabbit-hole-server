package handler

import (
	"errors"
	"strconv"

	"github.com/gin-gonic/gin"

	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

func passwordBusy(c *gin.Context, err error) bool {
	if !errors.Is(err, service.ErrPasswordHashBusy) {
		return false
	}
	retryAfter := service.PasswordHashRetryAfter()
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	response.HandleError(c, httperrors.ServiceUnavailable("SERVER_BUSY", "the server is busy, retry shortly", err).
		WithParams(map[string]any{"retry_after": retryAfter}))
	return true
}
