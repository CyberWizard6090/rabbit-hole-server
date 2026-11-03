package service

import (
	"strings"
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

func TestBurnPasswordHashIsSafeAndRepeatable(t *testing.T) {
	burnPasswordHash("anything")
	burnPasswordHash("anything else")
	if !strings.HasPrefix(dummyHash, "$argon2id$") || !verifyPassword("timing-equalizer-not-a-real-password", dummyHash) {
		t.Fatalf("dummy hash is not a valid argon2id hash: %q", dummyHash)
	}
}

func TestTokenParserRejectsTokensWithoutExpiryOrWrongAlgorithm(t *testing.T) {
	svc := NewAuthService(nil, "12345678901234567890123456789012", time.Hour, time.Hour)
	sign := func(method jwt.SigningMethod, claims Claims, key any) string {
		t.Helper()
		value, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	valid := sign(jwt.SigningMethodHS256, Claims{UserID: 1, TokenType: TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}, svc.jwtSecret)
	if _, err := svc.ParseToken(valid); err != nil {
		t.Fatalf("control token rejected: %v", err)
	}

	noExpiry := sign(jwt.SigningMethodHS256, Claims{UserID: 1, TokenType: TokenAccess}, svc.jwtSecret)
	if _, err := svc.ParseToken(noExpiry); err == nil {
		t.Error("token without exp must be rejected")
	}

	hs512 := sign(jwt.SigningMethodHS512, Claims{UserID: 1, TokenType: TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}, svc.jwtSecret)
	if _, err := svc.ParseToken(hs512); err == nil {
		t.Error("HS512 token must be rejected: only HS256 is accepted")
	}

	none := sign(jwt.SigningMethodNone, Claims{UserID: 1, TokenType: TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}, jwt.UnsafeAllowNoneSignatureType)
	if _, err := svc.ParseToken(none); err == nil {
		t.Error(`alg "none" token must be rejected`)
	}
}
