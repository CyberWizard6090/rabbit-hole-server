package domain

import (
	"errors"
	"time"
)

var (
	ErrEmailTaken          = errors.New("email is already registered")
	ErrUsernameTaken       = errors.New("username is already registered")
	ErrContactAlreadyAdded = errors.New("contact already added")
)

type User struct {
	ID            uint          `gorm:"primaryKey" json:"id"`
	Email         string        `gorm:"uniqueIndex;not null" json:"email" binding:"required,email"`
	PasswordHash  string        `gorm:"not null" json:"-"`
	Username      string        `gorm:"uniqueIndex" json:"username" binding:"omitempty,min=3,max=30"`
	FirstName     string        `json:"first_name"`
	LastName      string        `json:"last_name"`
	AvatarURL     string        `json:"avatar_url"`
	Bio           string        `gorm:"type:text" json:"bio"`
	TimeZone      string        `gorm:"not null;default:'UTC'" json:"time_zone"`
	Contacts      []*User       `gorm:"many2many:user_contacts;foreignKey:ID;joinForeignKey:UserID;References:ID;joinReferences:ContactID" json:"contacts"`
	CreatedAt     time.Time     `gorm:"not null" json:"created_at"`
	UpdatedAt     time.Time     `gorm:"not null" json:"updated_at"`
	RefreshTokens []UserSession `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE;" json:"-"`
}

type UserSession struct {
	ID        uint      `gorm:"primaryKey" json:"-"`
	UserID    uint      `gorm:"not null;index" json:"-"`
	TokenHash string    `gorm:"unique;not null" json:"-"`
	ExpiresAt time.Time `gorm:"not null" json:"-"`
	CreatedAt time.Time `gorm:"not null" json:"-"`
}

type UserRepository interface {
	GetByID(id uint) (*User, error)
	Update(user *User) error
}
