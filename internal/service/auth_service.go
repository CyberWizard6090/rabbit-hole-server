package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/argon2"
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrWeakPassword       = errors.New("password must be between 8 and 128 characters")
	ErrTokenRevoked       = errors.New("token has been revoked")
	ErrOwnsWorkspaces     = errors.New("transfer or delete your workspaces before deleting the account")
)

const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

type AuthUserStore interface {
	CreateUser(user *domain.User) error
	GetByEmail(email string) (*domain.User, error)
	GetByID(id uint) (*domain.User, error)
	GetTokenVersion(userID uint) (int, error)
	CreateSession(session *domain.UserSession) error
	DeleteSession(tokenHash string) error
	RotateSession(tokenHash string, replacement *domain.UserSession) error
	RevokeAll(userID uint) (int, error)
	HasSessionCreatedSince(userID uint, since time.Time) (bool, error)
	UpdatePasswordAndRevoke(userID uint, passwordHash string) (int, error)
	CountOwnedWorkspaces(userID uint) (int64, error)
	Anonymize(userID uint) error
}

func validatePassword(password string) error {
	if n := len([]rune(password)); n < MinPasswordLength || n > MaxPasswordLength {
		return ErrWeakPassword
	}
	return nil
}

type TokenType string

const (
	TokenAccess  TokenType = "access"
	TokenRefresh TokenType = "refresh"
)

type Claims struct {
	UserID       uint      `json:"user_id"`
	TokenType    TokenType `json:"token_type"`
	TokenVersion int       `json:"tv"`
	jwt.RegisteredClaims
}

type RefreshClaims = Claims

var tokenParser = jwt.NewParser(
	jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
	jwt.WithExpirationRequired(),
)

func (s *AuthService) keyFunc(*jwt.Token) (interface{}, error) { return s.jwtSecret, nil }

type AuthService struct {
	repo       AuthUserStore
	jwtSecret  []byte
	jwtTTL     time.Duration
	refreshTTL time.Duration
}

func NewAuthService(r AuthUserStore, jwtSecret string, accessTTL, refreshTTL time.Duration) *AuthService {
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

	const maxHashLength = 1024

	decode := base64.RawStdEncoding.DecodeString
	salt, err := decode(parts[4])
	if err != nil || len(salt) == 0 {
		return false
	}
	expected, err := decode(parts[5])
	if err != nil || len(expected) == 0 || len(expected) > maxHashLength {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, iterations, memory, uint8(parallelism), uint32(len(expected))) //nolint:gosec // length is bounded by maxHashLength
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
	if err := validatePassword(password); err != nil {
		return err
	}
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

var (
	dummyHashOnce sync.Once
	dummyHash     string
)

func burnPasswordHash(password string) {
	dummyHashOnce.Do(func() {
		dummyHash, _ = hashPassword("timing-equalizer-not-a-real-password")
	})
	_ = verifyPassword(password, dummyHash)
}

func (s *AuthService) Login(email, password string) (dto.TokenPair, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	user, err := s.repo.GetByEmail(email)
	if err != nil {
		burnPasswordHash(password)
		return dto.TokenPair{}, ErrInvalidCredentials
	}

	if !verifyPassword(password, user.PasswordHash) {
		return dto.TokenPair{}, ErrInvalidCredentials
	}

	return s.issueTokenPair(user.ID, user.TokenVersion)
}

func (s *AuthService) GenerateTokenPair(userID uint) (dto.TokenPair, error) {
	version, err := s.repo.GetTokenVersion(userID)
	if err != nil {
		return dto.TokenPair{}, err
	}
	return s.issueTokenPair(userID, version)
}

func (s *AuthService) issueTokenPair(userID uint, version int) (dto.TokenPair, error) {
	pair, session, err := s.generateTokenPair(userID, version)
	if err != nil {
		return dto.TokenPair{}, err
	}
	if err := s.repo.CreateSession(session); err != nil {
		return dto.TokenPair{}, err
	}
	return pair, nil
}

func (s *AuthService) generateTokenPair(userID uint, version int) (dto.TokenPair, *domain.UserSession, error) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return dto.TokenPair{}, nil, fmt.Errorf("generate token id: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)

	accessClaims := Claims{
		UserID:       userID,
		TokenType:    TokenAccess,
		TokenVersion: version,
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
	token, err := tokenParser.ParseWithClaims(refreshTokenString, &RefreshClaims{}, s.keyFunc)
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
	version, err := s.repo.GetTokenVersion(claims.UserID)
	if err != nil {
		return dto.TokenPair{}, errors.New("session not found")
	}
	pair, replacement, err := s.generateTokenPair(claims.UserID, version)
	if err != nil {
		return dto.TokenPair{}, err
	}
	if err := s.repo.RotateSession(tokenHash, replacement); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			s.handleUnknownRefreshSession(claims.UserID)
			return dto.TokenPair{}, errors.New("session not found")
		}
		return dto.TokenPair{}, fmt.Errorf("rotate refresh session: %w", err)
	}
	return pair, nil
}

const refreshReuseGrace = 10 * time.Second

func (s *AuthService) handleUnknownRefreshSession(userID uint) {
	recent, err := s.repo.HasSessionCreatedSince(userID, time.Now().Add(-refreshReuseGrace))
	if err != nil || recent {
		return
	}
	if _, err := s.repo.RevokeAll(userID); err != nil {
		log.Printf("revoke sessions after refresh token reuse (user %d): %v", userID, err)
		return
	}
	log.Printf("refresh token reuse detected: revoked all sessions of user %d", userID)
}

func (s *AuthService) Logout(refreshTokenString string) error {
	tokenHash := s.hashToken(refreshTokenString)
	return s.repo.DeleteSession(tokenHash)
}

func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	token, err := tokenParser.ParseWithClaims(tokenString, &Claims{}, s.keyFunc)
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid && claims.TokenType == TokenAccess {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

func (s *AuthService) Authenticate(tokenString string) (*Claims, error) {
	claims, err := s.ParseToken(tokenString)
	if err != nil {
		return nil, err
	}
	current, err := s.repo.GetTokenVersion(claims.UserID)
	if err != nil || current != claims.TokenVersion {
		return nil, ErrTokenRevoked
	}
	return claims, nil
}

func (s *AuthService) ChangePassword(userID uint, currentPassword, newPassword string) (dto.TokenPair, error) {
	if err := validatePassword(newPassword); err != nil {
		return dto.TokenPair{}, err
	}
	user, err := s.repo.GetByID(userID)
	if err != nil || !verifyPassword(currentPassword, user.PasswordHash) {
		return dto.TokenPair{}, ErrInvalidCredentials
	}
	hash, err := hashPassword(newPassword)
	if err != nil {
		return dto.TokenPair{}, err
	}
	version, err := s.repo.UpdatePasswordAndRevoke(userID, hash)
	if err != nil {
		return dto.TokenPair{}, err
	}
	return s.issueTokenPair(userID, version)
}

func (s *AuthService) LogoutAll(userID uint) error {
	_, err := s.repo.RevokeAll(userID)
	return err
}

func (s *AuthService) DeleteAccount(userID uint, password string) error {
	user, err := s.repo.GetByID(userID)
	if err != nil || !verifyPassword(password, user.PasswordHash) {
		return ErrInvalidCredentials
	}
	owned, err := s.repo.CountOwnedWorkspaces(userID)
	if err != nil {
		return err
	}
	if owned > 0 {
		return ErrOwnsWorkspaces
	}
	return s.repo.Anonymize(userID)
}
