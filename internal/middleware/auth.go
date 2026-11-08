package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

func AuthMiddleware(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			response.Unauthorized(c)
			c.Abort()
			return
		}

		if !strings.HasPrefix(header, "Bearer ") {
			response.Fail(c, http.StatusUnauthorized, "ACCESS_TOKEN_INVALID", nil)
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(header, "Bearer ")

		claims, err := authService.Authenticate(c.Request.Context(), tokenString)
		if err != nil {
			// An expired token is the signal to refresh quietly; anything else
			// means the session is gone and the user must log in again.
			code := "ACCESS_TOKEN_INVALID"
			if errors.Is(err, jwt.ErrTokenExpired) {
				code = "ACCESS_TOKEN_EXPIRED"
			}
			response.Fail(c, http.StatusUnauthorized, code, nil)
			c.Abort()
			return
		}

		c.Set("userID", claims.UserID)
		c.Next()
	}
}
