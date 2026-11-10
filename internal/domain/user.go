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
	ErrInvalidToken        = errors.New("the link is invalid, expired or already used")
)

type User struct {
	ID              uint          `json:"id"`
	Email           string        `json:"email" binding:"required,email"`
	PasswordHash    string        `json:"-"`
	Username        string        `json:"username" binding:"omitempty,min=3,max=30"`
	FirstName       string        `json:"first_name"`
	LastName        string        `json:"last_name"`
	AvatarURL       string        `json:"avatar_url"`
	Bio             string        `json:"bio"`
	TimeZone        string        `json:"time_zone"`
	TokenVersion    int           `json:"-"`
	EmailVerifiedAt *time.Time    `json:"email_verified_at"`
	PendingEmail    string        `json:"pending_email,omitempty"`
	Contacts        []*User       `json:"contacts"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	RefreshTokens   []UserSession `json:"-"`
}

type UserSession struct {
	ID        uint       `json:"-"`
	UserID    uint       `json:"-"`
	TokenHash string     `json:"-"`
	FamilyID  string     `json:"-"`
	RotatedAt *time.Time `json:"-"`
	ExpiresAt time.Time  `json:"-"`
	CreatedAt time.Time  `json:"-"`
	UserAgent string     `json:"-"`
	IP        string     `json:"-"`
	LoginAt   time.Time  `json:"-"`
}

// SessionInfo is one signed-in device as the owner sees it. ID is the opaque family id of the
// refresh tokens; it is only ever shown to the user who owns it.
type SessionInfo struct {
	ID         string    `json:"id"`
	Device     string    `json:"device"`
	UserAgent  string    `json:"user_agent"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsedAt time.Time `json:"last_used_at"`
	Current    bool      `json:"current"`
}

type SessionReplay struct {
	FamilyID string
	Age      time.Duration
}

func (e *SessionReplay) Error() string { return "refresh token was already rotated" }

type UserRepository interface {
	GetByID(ctx context.Context, id uint) (*User, error)
	Update(ctx context.Context, user *User) error
}
