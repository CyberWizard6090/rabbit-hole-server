package domain

import (
	"gorm.io/gorm"
)

type Space struct {
	gorm.Model
	Name        string `gorm:"not null" json:"name"`
	WorkspaceID uint   `gorm:"not null;index;column:workspace_id" json:"workspace_id"`

	Currency string `gorm:"default:'USD'" json:"currency"`
	OwnerID  uint   `gorm:"not null" json:"owner_id"`

	Lists []List `gorm:"foreignKey:SpaceID;references:ID;constraint:OnDelete:CASCADE" json:"lists"`
	Tasks []Task `gorm:"foreignKey:SpaceID;references:ID;constraint:OnDelete:CASCADE" json:"tasks"`
	Tags  []Tag  `gorm:"foreignKey:SpaceID;constraint:OnDelete:CASCADE" json:"tags"`
}

type Folder struct {
	gorm.Model
	Name     string   `gorm:"size:255;not null"`
	SpaceID  uint     `gorm:"not null;index"`
	ParentID *uint    `gorm:"index"`
	Parent   *Folder  `gorm:"foreignKey:ParentID;constraint:OnDelete:CASCADE;"`
	Children []Folder `gorm:"foreignKey:ParentID;constraint:OnDelete:CASCADE;"`
	Lists    []List   `gorm:"constraint:OnDelete:CASCADE;"`
}

type List struct {
	gorm.Model
	Name           string       `gorm:"not null" json:"name"`
	SpaceID        uint         `gorm:"index;not null;column:space_id" json:"space_id"`
	FolderID       *uint        `gorm:"index"`
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

type TaskStatus struct {
	gorm.Model
	SpaceID  uint       `gorm:"index;not null;column:space_id" json:"-"`
	ListID   uint       `gorm:"index;not null;column:list_id" json:"list_id"`
	Name     string     `gorm:"not null" json:"name"`
	Color    string     `gorm:"size:7;not null" json:"color"`
	Position int        `gorm:"not null" json:"position"`
	Type     StatusType `gorm:"type:smallint;not null" json:"type"`
}

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
