package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"
)

const WSTicketKey = "ws_ticket"

func ExtractWSTicket() gin.HandlerFunc {
	return func(c *gin.Context) {
		if strings.HasSuffix(c.Request.URL.Path, "/events") {
			query := c.Request.URL.Query()
			if ticket := query.Get("ticket"); ticket != "" {
				c.Set(WSTicketKey, ticket)
				query.Del("ticket")
				c.Request.URL.RawQuery = query.Encode()
			}
		}
		c.Next()
	}
}
