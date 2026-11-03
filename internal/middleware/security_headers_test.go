package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func serveWithSecurityHeaders(t *testing.T, hsts bool) http.Header {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(SecurityHeaders(hsts))
	r.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Header()
}

func TestSecurityHeadersAlwaysSet(t *testing.T) {
	h := serveWithSecurityHeaders(t, false)
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
		"Cache-Control":           "no-store",
	}
	for k, v := range want {
		if got := h.Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if h.Get("X-XSS-Protection") != "" {
		t.Error("deprecated X-XSS-Protection must not be set")
	}
}

func TestSecurityHeadersHSTSOnlyWhenEnabled(t *testing.T) {
	if got := serveWithSecurityHeaders(t, false).Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS sent while disabled: %q", got)
	}
	if got := serveWithSecurityHeaders(t, true).Get("Strict-Transport-Security"); got == "" {
		t.Error("HSTS missing while enabled")
	}
}
