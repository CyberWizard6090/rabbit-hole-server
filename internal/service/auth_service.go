package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	"rabbit-hole-server/internal/repository"
)

var ErrInvalidCredentials = errors.New("invalid email or password")

type Claims struct {
	UserID uint `json:"user_id"`
	jwt.RegisteredClaims
}

type RefreshClaims struct {
	UserID uint `json:"user_id"`
	jwt.RegisteredClaims
}

type AuthService struct {
	repo      *repository.UserRepository
	jwtSecret []byte
	pepper    []byte
}

func NewAuthService(r *repository.UserRepository, jwtSecret, pepper string) *AuthService {
	return &AuthService{
		repo:      r,
		jwtSecret: []byte(jwtSecret),
		pepper:    []byte(pepper),
	}
}

func (s *AuthService) preparePassword(password string) string {
	h := hmac.New(sha256.New, s.pepper)
	h.Write([]byte(password))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *AuthService) hashToken(token string) string {
	h := sha256.New()
	h.Write([]byte(token))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *AuthService) Register(email, password, username string) error {
	preparedPassword := s.preparePassword(password)

	hashedBytes, err := bcrypt.GenerateFromPassword([]byte(preparedPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	user := &domain.User{
		Email:        email,
		PasswordHash: string(hashedBytes),
		Username:     username,
	}
	if user.Username == "" {
		user.Username = fmt.Sprintf("user_%d", time.Now().UnixNano())
	}

	return s.repo.CreateUser(user)
}

func (s *AuthService) Login(email, password string) (dto.TokenPair, error) {
	user, err := s.repo.GetByEmail(email)
	if err != nil {
		return dto.TokenPair{}, ErrInvalidCredentials
	}

	preparedPassword := s.preparePassword(password)

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(preparedPassword))
	if err != nil {
		return dto.TokenPair{}, ErrInvalidCredentials
	}

	return s.GenerateTokenPair(user.ID)
}

func (s *AuthService) GenerateTokenPair(userID uint) (dto.TokenPair, error) {
	accessClaims := Claims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	accessToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString(s.jwtSecret)
	if err != nil {
		return dto.TokenPair{}, err
	}

	refreshExpiresAt := time.Now().Add(7 * 24 * time.Hour)
	refreshClaims := RefreshClaims{
		UserID: userID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(refreshExpiresAt),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	refreshToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString(s.jwtSecret)
	if err != nil {
		return dto.TokenPair{}, err
	}

	session := &domain.UserSession{
		UserID:    userID,
		TokenHash: s.hashToken(refreshToken),
		ExpiresAt: refreshExpiresAt,
	}

	if err := s.repo.CreateSession(session); err != nil {
		return dto.TokenPair{}, err
	}

	return dto.TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *AuthService) Refresh(refreshTokenString string) (dto.TokenPair, error) {
	token, err := jwt.ParseWithClaims(refreshTokenString, &RefreshClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
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

	tokenHash := s.hashToken(refreshTokenString)
	session, err := s.repo.FindSession(tokenHash)
	if err != nil {
		return dto.TokenPair{}, errors.New("session not found")
	}

	_ = s.repo.DeleteSession(session.TokenHash)

	return s.GenerateTokenPair(claims.UserID)
}

func (s *AuthService) Logout(refreshTokenString string) error {
	tokenHash := s.hashToken(refreshTokenString)
	return s.repo.DeleteSession(tokenHash)
}

func (s *AuthService) ParseToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return s.jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}
