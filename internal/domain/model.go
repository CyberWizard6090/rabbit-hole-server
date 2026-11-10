package domain

import (
	"errors"
	"time"
)

var (
	ErrNotFound       = errors.New("record not found")
	ErrOwnsWorkspaces = errors.New("transfer or delete your workspaces before deleting the account")
)

type Model struct {
	ID        uint       `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-"`
}
