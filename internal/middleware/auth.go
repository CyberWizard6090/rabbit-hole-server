package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	contextutil "rabbit-hole-server/internal/http/context"
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
			code := "ACCESS_TOKEN_INVALID"
			if errors.Is(err, jwt.ErrTokenExpired) {
				code = "ACCESS_TOKEN_EXPIRED"
			}
			response.Fail(c, http.StatusUnauthorized, code, nil)
			c.Abort()
			return
		}

		c.Set("userID", claims.UserID)
		c.Set(contextutil.TokenVersionKey, claims.TokenVersion)
		c.Next()
	}
}

func OptionalAuthMiddleware(authService *service.AuthService) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			c.Next()
			return
		}
		claims, err := authService.Authenticate(c.Request.Context(), strings.TrimPrefix(header, "Bearer "))
		if err == nil {
			c.Set("userID", claims.UserID)
			c.Set(contextutil.TokenVersionKey, claims.TokenVersion)
		}
		c.Next()
	}
}
