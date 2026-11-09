package domain

import (
	"context"
	"errors"
)

var ErrParentStatusNotFound = errors.New("parent status not found in this space")

type Space struct {
	Model
	Name        string `json:"name"`
	WorkspaceID uint   `json:"workspace_id"`

	Currency string `json:"currency"`
	OwnerID  uint   `json:"owner_id"`

	Lists []List `json:"lists"`
	Tasks []Task `json:"tasks"`
	Tags  []Tag  `json:"tags"`
}

type Folder struct {
	Model
	Name     string   `json:"name"`
	SpaceID  uint     `json:"space_id"`
	ParentID *uint    `json:"parent_id"`
	Parent   *Folder  `json:"-"`
	Children []Folder `json:"-"`
	Lists    []List   `json:"-"`
}

type List struct {
	Model
	Name           string       `json:"name"`
	SpaceID        uint         `json:"space_id"`
	FolderID       *uint        `json:"folder_id"`
	Statuses       []TaskStatus `json:"statuses"`
	Tasks          []Task       `json:"tasks"`
	TaskCount      int          `json:"task_count"`
	ParentStatusID *uint        `json:"parent_status_id,omitempty"`
}

type StatusType int

const (
	StatusTypeUndefined StatusType = iota
	StatusTodo
	StatusInProgress
	StatusDone
)

type TaskStatus struct {
	Model
	SpaceID  uint       `json:"-"`
	ListID   uint       `json:"list_id"`
	Name     string     `json:"name"`
	Color    string     `json:"color"`
	Position int        `json:"position"`
	Type     StatusType `json:"type"`
}

var (
	ErrStatusInUse              = errors.New("status is still used by tasks; pass replacement_id to move them")
	ErrInvalidReplacementStatus = errors.New("replacement status must be a different status of the same list")
)

type FolderRepository interface {
	Create(ctx context.Context, folder *Folder) error
	GetByID(ctx context.Context, id uint) (*Folder, error)
	GetAllBySpace(ctx context.Context, spaceID uint) ([]Folder, error)
	Update(ctx context.Context, folder *Folder) error
	Delete(ctx context.Context, id uint) error
}

type SpaceRepository interface {
	GetByID(ctx context.Context, id uint) (*Space, error)
	Create(ctx context.Context, space *Space) error
	CreateWithDefaults(ctx context.Context, space *Space, list *List, statuses []TaskStatus) error
	Update(ctx context.Context, space *Space) error
	Delete(ctx context.Context, id uint) error
	GetAll(ctx context.Context, workspaceID uint, limit, offset int) ([]Space, int64, error)
	GetDashboard(ctx context.Context, spaceID uint, tasksPerList int) (*Space, error)
	CreateStatus(ctx context.Context, status *TaskStatus) error
	CreateTag(ctx context.Context, tag *Tag) error

	CreateList(ctx context.Context, list *List) error
	GetListByID(ctx context.Context, id uint) (*List, error)
	GetListsBySpace(ctx context.Context, spaceID uint) ([]List, error)
	GetStatusByID(ctx context.Context, id uint) (*TaskStatus, error)
	UpdateStatus(ctx context.Context, status *TaskStatus) error

	GetListsByFolder(ctx context.Context, folderID uint) ([]List, error)
	UpdateList(ctx context.Context, list *List) error
	DeleteList(ctx context.Context, id uint) error
}

type StatusRepository interface {
	Create(ctx context.Context, status *TaskStatus) error
	GetAllByList(ctx context.Context, listID uint) ([]TaskStatus, error)
	GetByID(ctx context.Context, statusID uint) (*TaskStatus, error)
	Update(ctx context.Context, status *TaskStatus) error
	UpdatePosition(ctx context.Context, status *TaskStatus, position int) error
	Delete(ctx context.Context, listID uint, statusID uint, replacementID *uint) error
}
