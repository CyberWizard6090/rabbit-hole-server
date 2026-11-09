package middleware

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/http/response"
)

func PathIDs() gin.HandlerFunc {
	return func(c *gin.Context) {
		for _, param := range c.Params {
			if !strings.HasSuffix(param.Key, "_id") && param.Key != "id" {
				continue
			}
			if _, err := strconv.ParseInt(param.Value, 10, 64); err != nil && isDigits(param.Value) {
				response.InvalidID(c, param.Key)
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
