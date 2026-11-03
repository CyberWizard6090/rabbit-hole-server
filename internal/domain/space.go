package domain

import (
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
	Create(folder *Folder) error
	GetByID(id uint) (*Folder, error)
	GetAllBySpace(spaceID uint) ([]Folder, error)
	Update(folder *Folder) error
	Delete(id uint) error
}

type SpaceRepository interface {
	GetByID(id uint) (*Space, error)
	Create(space *Space) error
	CreateWithDefaults(space *Space, list *List, statuses []TaskStatus) error
	GetAll(workspaceID uint, limit, offset int) ([]Space, int64, error)
	GetDashboard(spaceID uint) (*Space, error)
	CreateStatus(status *TaskStatus) error
	CreateTag(tag *Tag) error

	CreateList(list *List) error
	GetListByID(id uint) (*List, error)
	GetListsBySpace(spaceID uint) ([]List, error)
	GetStatusByID(id uint) (*TaskStatus, error)
	UpdateStatus(status *TaskStatus) error

	GetListsByFolder(folderID uint) ([]List, error)
	UpdateList(list *List) error
	DeleteList(id uint) error
}

type StatusRepository interface {
	Create(status *TaskStatus) error
	GetAllByList(listID uint) ([]TaskStatus, error)
	GetByID(statusID uint) (*TaskStatus, error)
	Update(status *TaskStatus) error
	UpdatePosition(status *TaskStatus, position int) error
	Delete(listID uint, statusID uint, replacementID *uint) error
}
