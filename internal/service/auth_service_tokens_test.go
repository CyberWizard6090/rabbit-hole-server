package service

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestAuthServiceGenerateTokenPairCreatesMatchingSession(t *testing.T) {
	svc := NewAuthService(nil, "12345678901234567890123456789012", time.Hour, 24*time.Hour)
	before := time.Now().Add(-time.Second)

	pair, session, err := svc.generateTokenPair(42)
	if err != nil {
		t.Fatalf("generateTokenPair error=%v", err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" {
		t.Fatal("expected both access and refresh tokens")
	}
	if pair.AccessToken == pair.RefreshToken {
		t.Fatal("access and refresh tokens must differ")
	}
	if session.UserID != 42 {
		t.Fatalf("session user id=%d want 42", session.UserID)
	}
	if session.TokenHash != svc.hashToken(pair.RefreshToken) {
		t.Fatal("session hash does not match refresh token")
	}
	if session.ExpiresAt.Before(before) {
		t.Fatal("refresh session expiration is unexpectedly old")
	}
	if time.Until(session.ExpiresAt) < 23*time.Hour {
		t.Fatalf("refresh session TTL is too short: %s", time.Until(session.ExpiresAt))
	}
}

func TestAuthServiceParseTokenAcceptsAccessOnly(t *testing.T) {
	svc := NewAuthService(nil, "12345678901234567890123456789012", time.Hour, time.Hour)
	pair, _, err := svc.generateTokenPair(99)
	if err != nil {
		t.Fatalf("generateTokenPair error=%v", err)
	}

	claims, err := svc.ParseToken(pair.AccessToken)
	if err != nil {
		t.Fatalf("ParseToken error=%v", err)
	}
	if claims.UserID != 99 || claims.TokenType != TokenAccess {
		t.Fatalf("claims=%+v", claims)
	}

	if _, err := svc.ParseToken(pair.RefreshToken); err == nil {
		t.Fatal("refresh token must be rejected by ParseToken")
	}
}

func TestAuthServiceParseTokenRejectsExpiredAndWrongSignature(t *testing.T) {
	secret := "12345678901234567890123456789012"
	svc := NewAuthService(nil, secret, time.Hour, time.Hour)

	expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: 1, TokenType: TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute))},
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseToken(expired); err == nil {
		t.Fatal("expired token must be rejected")
	}

	wrongKey, err := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{
		UserID: 1, TokenType: TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}).SignedString([]byte("different-secret-12345678901234567890"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseToken(wrongKey); err == nil {
		t.Fatal("token with wrong signature must be rejected")
	}
}

func TestAuthServiceParseTokenRejectsUnsupportedAlgorithm(t *testing.T) {
	secret := "12345678901234567890123456789012"
	svc := NewAuthService(nil, secret, time.Hour, time.Hour)
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS384, Claims{
		UserID: 1, TokenType: TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	}).SignedString([]byte(secret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ParseToken(token); err == nil {
		t.Fatal("HS384 token must be rejected")
	}
}

func TestAuthServiceHashTokenIsSHA256(t *testing.T) {
	svc := NewAuthService(nil, "secret", time.Hour, time.Hour)
	token := "refresh-token"
	wantBytes := sha256.Sum256([]byte(token))
	want := hex.EncodeToString(wantBytes[:])
	if got := svc.hashToken(token); got != want {
		t.Fatalf("hashToken=%q want %q", got, want)
	}
}

func TestAuthServiceRefreshTTLReturnsConfiguredValue(t *testing.T) {
	ttl := 12 * time.Hour
	svc := NewAuthService(nil, "secret", time.Hour, ttl)
	if got := svc.RefreshTTL(); got != ttl {
		t.Fatalf("RefreshTTL=%s want %s", got, ttl)
	}
}

func TestAuthServiceTokenClaimsHaveExpectedTypes(t *testing.T) {
	access := Claims{UserID: 1, TokenType: TokenAccess}
	refresh := RefreshClaims{UserID: 1, TokenType: TokenRefresh}
	if access.TokenType == refresh.TokenType {
		t.Fatal("access and refresh types must differ")
	}
}

func TestVerifyPasswordRejectsUnsafeArgon2Parameters(t *testing.T) {
	valid, err := hashPassword("password")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(valid, "$")
	parts[3] = "m=1048577,t=3,p=4"
	if verifyPassword("password", strings.Join(parts, "$")) {
		t.Fatal("excessive memory parameter must be rejected")
	}

	parts = strings.Split(valid, "$")
	parts[3] = "m=65536,t=11,p=4"
	if verifyPassword("password", strings.Join(parts, "$")) {
		t.Fatal("excessive iteration parameter must be rejected")
	}

	parts = strings.Split(valid, "$")
	parts[3] = "m=65536,t=3,p=17"
	if verifyPassword("password", strings.Join(parts, "$")) {
		t.Fatal("excessive parallelism parameter must be rejected")
	}
}
