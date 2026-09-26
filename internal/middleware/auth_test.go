package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"rabbit-hole-server/internal/service"
)

const testJWTSecret = "12345678901234567890123456789012"

func testRouterWithAuth(t *testing.T) (*gin.Engine, *int) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	auth := service.NewAuthService(nil, testJWTSecret, time.Hour, 24*time.Hour)
	router.Use(AuthMiddleware(auth))
	calls := 0
	router.GET("/protected", func(c *gin.Context) {
		calls++
		uid, ok := c.Get("userID")
		if !ok {
			t.Fatal("userID not stored")
		}
		if uid != uint(42) {
			t.Fatalf("userID=%v", uid)
		}
		c.Status(http.StatusNoContent)
	})
	return router, &calls
}

func signAccessTestToken(t *testing.T, userID uint) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, service.Claims{
		UserID:           userID,
		TokenType:        service.TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	})
	value, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestAuthMiddlewareAcceptsValidAccessToken(t *testing.T) {
	router, calls := testRouterWithAuth(t)
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+signAccessTestToken(t, 42))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent || *calls != 1 {
		t.Fatalf("status=%d calls=%d", rec.Code, *calls)
	}
}

func TestAuthMiddlewareRejectsInvalidHeaders(t *testing.T) {
	cases := []struct{ name, header string }{
		{"missing", ""},
		{"wrong scheme", "Basic abc"},
		{"empty bearer", "Bearer "},
		{"malformed", "Bearer not-a-jwt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router, calls := testRouterWithAuth(t)
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if *calls != 0 {
				t.Fatalf("downstream calls=%d", *calls)
			}
		})
	}
}

func TestAuthMiddlewareRejectsRefreshTokenAndBadSignature(t *testing.T) {
	router, calls := testRouterWithAuth(t)
	refresh := jwt.NewWithClaims(jwt.SigningMethodHS256, service.RefreshClaims{
		UserID: 42, TokenType: service.TokenRefresh,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	})
	refreshValue, err := refresh.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}

	for _, token := range []string{refreshValue, signAccessTestToken(t, 42) + "tampered"} {
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status=%d", rec.Code)
		}
	}
	if *calls != 0 {
		t.Fatalf("downstream calls=%d", *calls)
	}
}

func TestAuthMiddlewareRejectsExpiredToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	auth := service.NewAuthService(nil, testJWTSecret, time.Hour, time.Hour)
	router.Use(AuthMiddleware(auth))
	calls := 0
	router.GET("/protected", func(c *gin.Context) { calls++; c.Status(http.StatusNoContent) })

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, service.Claims{
		UserID: 1, TokenType: service.TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))},
	})
	value, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+value)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || calls != 0 {
		t.Fatalf("status=%d calls=%d", rec.Code, calls)
	}
}
