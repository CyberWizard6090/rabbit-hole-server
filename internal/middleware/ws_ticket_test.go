package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func serveTicket(t *testing.T, target string) (ticket, rawQuery string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(ExtractWSTicket())
	r.GET("/*any", func(c *gin.Context) {
		ticket = c.GetString(WSTicketKey)
		rawQuery = c.Request.URL.RawQuery
		c.Status(http.StatusNoContent)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	return ticket, rawQuery
}

func TestExtractWSTicketMovesTheTicketOutOfTheURL(t *testing.T) {
	ticket, query := serveTicket(t, "/api/v1/lists/5/events?ticket=abc123&keep=1")
	if ticket != "abc123" {
		t.Fatalf("ticket = %q", ticket)
	}
	if query != "keep=1" {
		t.Fatalf("query after extraction = %q; the ticket must not be left in the URL (the access logger prints it)", query)
	}
}

func TestExtractWSTicketIgnoresOtherPaths(t *testing.T) {
	ticket, query := serveTicket(t, "/api/v1/workspaces?ticket=abc123")
	if ticket != "" || query != "ticket=abc123" {
		t.Fatalf("only handshake URLs ending in /events are touched, got ticket=%q query=%q", ticket, query)
	}
}

func TestExtractWSTicketWithoutATicket(t *testing.T) {
	ticket, query := serveTicket(t, "/api/v1/spaces/3/events")
	if ticket != "" || query != "" {
		t.Fatalf("ticket=%q query=%q", ticket, query)
	}
}
