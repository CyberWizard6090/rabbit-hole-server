package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	"rabbit-hole-server/internal/repository"
)

var ErrInvalidCredentials = errors.New("invalid email or password")

type TokenType string

const (
	TokenAccess  TokenType = "access"
	TokenRefresh TokenType = "refresh"
)

type Claims struct {
	UserID    uint      `json:"user_id"`
	TokenType TokenType `json:"token_type"`
	jwt.RegisteredClaims
}

type RefreshClaims struct {
	UserID    uint      `json:"user_id"`
	TokenType TokenType `json:"token_type"`
	jwt.RegisteredClaims
}

type AuthService struct {
	repo       *repository.UserRepository
	jwtSecret  []byte
	jwtTTL     time.Duration
	refreshTTL time.Duration
}

func NewAuthService(r *repository.UserRepository, jwtSecret string, accessTTL, refreshTTL time.Duration) *AuthService {
	return &AuthService{
		repo:       r,
		jwtSecret:  []byte(jwtSecret),
		jwtTTL:     accessTTL,
		refreshTTL: refreshTTL,
	}
}

func (s *AuthService) RefreshTTL() time.Duration {
	return s.refreshTTL
}

const (
	argon2Memory      = 64 * 1024
	argon2Iterations  = 3
	argon2Parallelism = 4
	argon2SaltLength  = 16
	argon2KeyLength   = 32
)

func hashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}

	key := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)
	encode := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2Memory,
		argon2Iterations,
		argon2Parallelism,
		encode(salt),
		encode(key),
	), nil
}

func verifyPassword(password, encodedHash string) bool {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}

	params := map[string]uint32{}
	for _, item := range strings.Split(parts[3], ",") {
		pair := strings.SplitN(item, "=", 2)
		if len(pair) != 2 {
			return false
		}
		value, err := strconv.ParseUint(pair[1], 10, 32)
		if err != nil {
			return false
		}
		params[pair[0]] = uint32(value)
	}
	memory, memoryOK := params["m"]
	iterations, iterationsOK := params["t"]
	parallelism, parallelismOK := params["p"]
	if !memoryOK || !iterationsOK || !parallelismOK || memory == 0 || iterations == 0 || parallelism == 0 {
		return false
	}
	if memory > 1024*1024 || iterations > 10 || parallelism > 16 {
		return false
	}

	decode := base64.RawStdEncoding.DecodeString
	salt, err := decode(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}
	expected, err := decode(parts[5])
	if err != nil || len(expected) == 0 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func (s *AuthService) hashToken(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *AuthService) Register(email, password, username string) error {
	email = strings.ToLower(strings.TrimSpace(email))
	username = strings.TrimSpace(username)
	passwordHash, err := hashPassword(password)
	if err != nil {
		return err
	}

	user := &domain.User{
		Email:        email,
		PasswordHash: passwordHash,
		Username:     username,
	}
	if user.Username == "" {
		user.Username = fmt.Sprintf("user_%d", time.Now().UnixNano())
	}

	return s.repo.CreateUser(user)
}

func (s *AuthService) Login(email, password string) (dto.TokenPair, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.repo.GetByEmail(email)
	if err != nil {
		return dto.TokenPair{}, ErrInvalidCredentials
	}

	if !verifyPassword(password, user.PasswordHash) {
		return dto.TokenPair{}, ErrInvalidCredentials
	}

	return s.GenerateTokenPair(user.ID)
}

func (s *AuthService) GenerateTokenPair(userID uint) (dto.TokenPair, error) {
	pair, session, err := s.generateTokenPair(userID)
	if err != nil {
		return dto.TokenPair{}, err
	}
	if err := s.repo.CreateSession(session); err != nil {
		return dto.TokenPair{}, err
	}
	return pair, nil
}

func (s *AuthService) generateTokenPair(userID uint) (dto.TokenPair, *domain.UserSession, error) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return dto.TokenPair{}, nil, fmt.Errorf("generate token id: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	accessClaims := Claims{
		UserID:    userID,
		TokenType: TokenAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        nonce,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(s.jwtTTL)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.jwtSecret)
	if err != nil {
		return dto.TokenPair{}, nil, err
	}

	refreshExpiresAt := time.Now().Add(s.refreshTTL)
	refreshClaims := RefreshClaims{
		UserID:    userID,
		TokenType: TokenRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        nonce + "-refresh",
			ExpiresAt: jwt.NewNumericDate(refreshExpiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.jwtSecret)
	if err != nil {
		return dto.TokenPair{}, nil, err
	}

	session := &domain.UserSession{
		UserID:    userID,
		TokenHash: s.hashToken(refreshToken),
		ExpiresAt: refreshExpiresAt,
	}

	return dto.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, session, nil
}

func (s *AuthService) Refresh(refreshTokenString string) (dto.TokenPair, error) {
	token, err := jwt.ParseWithClaims(refreshTokenString, &RefreshClaims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, jwt.ErrSignatureInvalid
		}
		return s.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return dto.TokenPair{}, errors.New("invalid or expired refresh token")
	}

	claims, ok := token.Claims.(*RefreshClaims)
	if !ok {
		return dto.TokenPair{}, errors.New("invalid token claims")
	}
	if claims.TokenType != TokenRefresh {
		return dto.TokenPair{}, errors.New("invalid token type")
	}

	tokenHash := s.hashToken(refreshTokenString)
	pair, replacement, err := s.generateTokenPair(claims.UserID)
	if err != nil {
		return dto.TokenPair{}, err
	}
	if err := s.repo.RotateSession(tokenHash, replacement); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return dto.TokenPair{}, errors.New("session not found")
		}
		return dto.TokenPair{}, fmt.Errorf("rotate refresh session: %w", err)
	}
	return pair, nil
}

func (s *AuthService) Logout(refreshTokenString string) error {
	tokenHash := s.hashToken(refreshTokenString)
	return s.repo.DeleteSession(tokenHash)
}

func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, jwt.ErrSignatureInvalid
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid && claims.TokenType == TokenAccess {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}
