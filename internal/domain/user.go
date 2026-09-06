package domain

import "time"

type User struct {
	ID            uint          `gorm:"primaryKey" json:"id"`
	Email         string        `gorm:"uniqueIndex;not null" json:"email" binding:"required,email"`
	PasswordHash  string        `gorm:"not null" json:"-"`
	Username      string        `gorm:"uniqueIndex" json:"username" binding:"omitempty,min=3,max=30"`
	FirstName     string        `json:"first_name"`
	LastName      string        `json:"last_name"`
	AvatarURL     string        `json:"avatar_url"`
	Bio           string        `gorm:"type:text" json:"bio"`
	TimeZone      string        `gorm:"default:'UTC'" json:"time_zone"`
	Contacts      []*User       `gorm:"many2many:user_contacts;foreignKey:ID;joinForeignKey:UserID;References:ID;joinReferences:ContactID" json:"contacts"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
	RefreshTokens []UserSession `gorm:"foreignKey:UserID;constraint:OnDelete:CASCADE;"`
}

type UserSession struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"not null;index"`
	TokenHash string    `gorm:"unique;not null"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time
}

type UserRepository interface {
	GetByID(id uint) (*User, error)
	Update(user *User) error
}
