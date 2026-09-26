package service

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAuthServiceParseTokenRejectsWrongAlgorithm(t *testing.T) {
	svc := NewAuthService(nil, "12345678901234567890123456789012", time.Hour, time.Hour)
	token := jwt.NewWithClaims(jwt.SigningMethodHS384, Claims{
		UserID:           1,
		TokenType:        TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	})
	value, err := token.SignedString(svc.jwtSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseToken(value); err == nil {
		t.Fatal("expected wrong algorithm to be rejected")
	}
}

func TestAuthServiceParseTokenRejectsWrongTokenType(t *testing.T) {
	svc := NewAuthService(nil, "12345678901234567890123456789012", time.Hour, time.Hour)
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID:           1,
		TokenType:        TokenRefresh,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	})
	value, err := token.SignedString(svc.jwtSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseToken(value); err == nil {
		t.Fatal("expected wrong token type to be rejected")
	}
}
