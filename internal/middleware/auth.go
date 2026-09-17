package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

func AuthMiddleware(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			response.Unauthorized(c)
			return
		}

		if !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, http.StatusUnauthorized, "Invalid token format")
			return
		}

		tokenString := strings.TrimPrefix(header, "Bearer ")

		claims, err := authService.ParseToken(tokenString)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, "Invalid or expired token")
			return
		}

		c.Set("userID", claims.UserID)
		c.Next()
	}
}
