package domain

import (
	"context"
	"errors"
	"time"
)

const (
	MaxSavedViewNameRunes  = 100
	MaxSavedViewFilters    = 20
	MaxSavedViewValueRunes = 500
	MaxSavedViewsPerUser   = 200
)

var (
	ErrViewNotFound     = errors.New("view not found")
	ErrViewLimitReached = errors.New("too many views")
	ErrViewForbidden    = errors.New("you cannot change this view")
	ErrInvalidView      = errors.New("invalid view")
)

type SavedView struct {
	ID          uint              `json:"id"`
	WorkspaceID uint              `json:"workspace_id"`
	ListID      *uint             `json:"list_id"`
	OwnerID     uint              `json:"-"`
	Owner       *CommentUser      `json:"owner"`
	IsOwner     bool              `json:"is_owner"`
	Name        string            `json:"name"`
	Shared      bool              `json:"shared"`
	Filters     map[string]string `json:"filters"`
	Sort        string            `json:"sort"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type SavedViewDraft struct {
	WorkspaceID uint
	ListID      *uint
	OwnerID     uint
	Name        string
	Shared      bool
	Filters     map[string]string
	Sort        string
}

type SavedViewUpdate struct {
	Name    *string
	Shared  *bool
	Filters map[string]string
	Sort    *string
}

type SavedViewRepository interface {
	Create(ctx context.Context, draft SavedViewDraft) (*SavedView, error)
	Get(ctx context.Context, workspaceID, id uint) (*SavedView, error)
	List(ctx context.Context, workspaceID, viewerID uint, listID *uint) ([]SavedView, error)
	Update(ctx context.Context, workspaceID, id uint, update SavedViewUpdate) (*SavedView, error)
	Delete(ctx context.Context, workspaceID, id uint) error
	CountOwned(ctx context.Context, workspaceID, ownerID uint) (int64, error)
	ListBelongsToWorkspace(ctx context.Context, workspaceID, listID uint) (bool, error)
}
