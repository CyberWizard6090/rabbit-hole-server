package pagination

import (
	"testing"

	"github.com/gin-gonic/gin"
	"net/http/httptest"
)

func TestParseUsesDefaults(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/items", nil)
	got := Parse(c)
	if got.Page != 1 || got.Limit != 10 || got.Offset != 0 {
		t.Fatalf("params=%+v", got)
	}
}

func TestParseAcceptsValidPageAndLimit(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/items?page=3&limit=25", nil)
	got := Parse(c)
	if got.Page != 3 || got.Limit != 25 || got.Offset != 50 {
		t.Fatalf("params=%+v", got)
	}
}

func TestParseFallsBackForInvalidValues(t *testing.T) {
	cases := []string{
		"/items?page=x&limit=25",
		"/items?page=0&limit=25",
		"/items?page=2&limit=0",
		"/items?page=2&limit=101",
		"/items?page=2&limit=x",
	}
	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest("GET", path, nil)
			got := Parse(c)
			if got.Limit < 1 || got.Limit > MaxLimit || got.Page < 1 {
				t.Fatalf("params=%+v", got)
			}
		})
	}
}
