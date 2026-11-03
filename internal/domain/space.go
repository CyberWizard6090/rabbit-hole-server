package domain

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
)

type Space struct {
	Model
	Name        string `gorm:"not null" json:"name"`
	WorkspaceID uint   `gorm:"not null;index;column:workspace_id" json:"workspace_id"`

	Currency string `gorm:"not null;default:'USD'" json:"currency"`
	OwnerID  uint   `gorm:"not null" json:"owner_id"`

	Lists []List `gorm:"foreignKey:SpaceID;references:ID;constraint:OnDelete:CASCADE" json:"lists"`
	Tasks []Task `gorm:"foreignKey:SpaceID;references:ID;constraint:OnDelete:CASCADE" json:"tasks"`
	Tags  []Tag  `gorm:"foreignKey:SpaceID;constraint:OnDelete:CASCADE" json:"tags"`
}

type Folder struct {
	Model
	Name     string   `gorm:"size:255;not null" json:"name"`
	SpaceID  uint     `gorm:"not null;index" json:"space_id"`
	ParentID *uint    `gorm:"index" json:"parent_id"`
	Parent   *Folder  `gorm:"foreignKey:ParentID;constraint:OnDelete:CASCADE;" json:"-"`
	Children []Folder `gorm:"foreignKey:ParentID;constraint:OnDelete:CASCADE;" json:"-"`
	Lists    []List   `gorm:"constraint:OnDelete:CASCADE;" json:"-"`
}

type List struct {
	Model
	Name           string       `gorm:"not null" json:"name"`
	SpaceID        uint         `gorm:"index;not null;column:space_id" json:"space_id"`
	FolderID       *uint        `gorm:"index" json:"folder_id"`
	Statuses       []TaskStatus `gorm:"foreignKey:ListID;constraint:OnDelete:CASCADE" json:"statuses"`
	Tasks          []Task       `gorm:"foreignKey:ListID" json:"tasks"`
	ParentStatusID *uint        `gorm:"column:parent_status_id;index" json:"parent_status_id,omitempty"`
}

type StatusType int

const (
	StatusTypeUndefined StatusType = iota
	StatusTodo
	StatusInProgress
	StatusDone
)

func (statusType StatusType) Value() (driver.Value, error) {
	switch statusType {
	case StatusTodo:
		return "todo", nil
	case StatusInProgress:
		return "in_progress", nil
	case StatusDone:
		return "done", nil
	default:
		return nil, fmt.Errorf("invalid task status type %d", statusType)
	}
}

func (statusType *StatusType) Scan(value any) error {
	var name string
	switch typedValue := value.(type) {
	case string:
		name = typedValue
	case []byte:
		name = string(typedValue)
	default:
		return fmt.Errorf("cannot scan task status type from %T", value)
	}

	switch name {
	case "todo":
		*statusType = StatusTodo
	case "in_progress":
		*statusType = StatusInProgress
	case "done":
		*statusType = StatusDone
	default:
		return fmt.Errorf("invalid task status type %q", name)
	}
	return nil
}

type TaskStatus struct {
	Model
	SpaceID  uint       `gorm:"index;not null;column:space_id" json:"-"`
	ListID   uint       `gorm:"index;not null;column:list_id" json:"list_id"`
	Name     string     `gorm:"not null" json:"name"`
	Color    string     `gorm:"size:7;not null" json:"color"`
	Position int        `gorm:"type:integer;not null" json:"position"`
	Type     StatusType `gorm:"type:task_status_type;not null" json:"type"`
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
	GetDashboard(ctx context.Context, spaceID uint) (*Space, error)
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
