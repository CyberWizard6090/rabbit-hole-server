package domain

import (
	"context"
	"errors"
	"time"
)

var (
	ErrEmailTaken          = errors.New("email is already registered")
	ErrUsernameTaken       = errors.New("username is already registered")
	ErrContactAlreadyAdded = errors.New("contact already added")
)

type User struct {
	ID            uint          `json:"id"`
	Email         string        `json:"email" binding:"required,email"`
	PasswordHash  string        `json:"-"`
	Username      string        `json:"username" binding:"omitempty,min=3,max=30"`
	FirstName     string        `json:"first_name"`
	LastName      string        `json:"last_name"`
	AvatarURL     string        `json:"avatar_url"`
	Bio           string        `json:"bio"`
	TimeZone      string        `json:"time_zone"`
	TokenVersion  int           `json:"-"`
	Contacts      []*User       `json:"contacts"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	RefreshTokens []UserSession `json:"-"`
}

type UserSession struct {
	ID        uint      `json:"-"`
	UserID    uint      `json:"-"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"-"`
	CreatedAt time.Time `json:"-"`
}

type UserRepository interface {
	GetByID(ctx context.Context, id uint) (*User, error)
	Update(ctx context.Context, user *User) error
}
