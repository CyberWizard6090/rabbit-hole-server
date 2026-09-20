package service

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestHashPasswordUsesArgon2id(t *testing.T) {
	hash, err := hashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("hash format = %q, want argon2id PHC format", hash)
	}
	if !verifyPassword("correct horse battery staple", hash) {
		t.Fatal("expected password verification to succeed")
	}
	if verifyPassword("wrong password", hash) {
		t.Fatal("expected wrong password verification to fail")
	}
}

func TestHashPasswordUsesUniqueSalts(t *testing.T) {
	first, err := hashPassword("same password")
	if err != nil {
		t.Fatalf("hash first password: %v", err)
	}
	second, err := hashPassword("same password")
	if err != nil {
		t.Fatalf("hash second password: %v", err)
	}
	if first == second {
		t.Fatal("expected separate password hashes to use different salts")
	}
}

func TestVerifyPasswordRejectsMalformedOrLegacyHash(t *testing.T) {
	for _, hash := range []string{"", "not-a-password-hash", "$2a$10$legacy-bcrypt-hash"} {
		if verifyPassword("password", hash) {
			t.Fatalf("expected hash %q to be rejected", hash)
		}
	}
}

func TestTokenTypesAreSeparated(t *testing.T) {
	service := NewAuthService(nil, "12345678901234567890123456789012", time.Hour, 24*time.Hour)
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: 42, TokenType: TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}).SignedString(service.jwtSecret)
	if err != nil {
		t.Fatalf("sign access token: %v", err)
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, RefreshClaims{
		UserID: 42, TokenType: TokenRefresh,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}).SignedString(service.jwtSecret)
	if err != nil {
		t.Fatalf("sign refresh token: %v", err)
	}

	claims, err := service.ParseToken(accessToken)
	if err != nil {
		t.Fatalf("parse access token: %v", err)
	}
	if claims.TokenType != TokenAccess {
		t.Fatalf("access token type = %q, want %q", claims.TokenType, TokenAccess)
	}
	if _, err := service.ParseToken(refreshToken); err == nil {
		t.Fatal("expected refresh token to be rejected as access token")
	}
	if _, err := service.Refresh(accessToken); err == nil {
		t.Fatal("expected access token to be rejected as refresh token")
	}
}
