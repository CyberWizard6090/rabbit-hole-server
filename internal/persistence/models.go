package persistence

import (
	"database/sql/driver"
	"fmt"
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID           uint   `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex;not null"`
	PasswordHash string `gorm:"not null"`
	Username     string `gorm:"uniqueIndex"`
	FirstName    string
	LastName     string
	AvatarURL    string
	Bio          string    `gorm:"type:text"`
	TimeZone     string    `gorm:"not null;default:'UTC'"`
	TokenVersion int       `gorm:"not null;default:0"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

type UserSession struct {
	ID        uint      `gorm:"primaryKey"`
	UserID    uint      `gorm:"not null;index"`
	TokenHash string    `gorm:"unique;not null"`
	ExpiresAt time.Time `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
}

type Workspace struct {
	gorm.Model
	Name        string `gorm:"size:255;not null"`
	OwnerID     uint   `gorm:"not null"`
	Description string `gorm:"type:text"`

	Members []Member `gorm:"foreignKey:WorkspaceID;constraint:OnDelete:CASCADE;"`
	Spaces  []Space  `gorm:"foreignKey:WorkspaceID;constraint:OnDelete:CASCADE;"`
	Roles   []Role   `gorm:"foreignKey:WorkspaceID;constraint:OnDelete:CASCADE;"`
}

type Member struct {
	gorm.Model

	WorkspaceID uint `gorm:"not null;index;uniqueIndex:idx_workspace_user,where:deleted_at IS NULL"`
	UserID      uint `gorm:"not null;index;uniqueIndex:idx_workspace_user,where:deleted_at IS NULL"`
	RoleID      uint `gorm:"not null;index"`

	Role Role `gorm:"foreignKey:RoleID;constraint:OnDelete:RESTRICT;"`
}

func (Member) TableName() string { return "workspace_members" }

type Role struct {
	gorm.Model

	WorkspaceID uint   `gorm:"not null;index;uniqueIndex:idx_roles_workspace_name,where:deleted_at IS NULL"`
	Name        string `gorm:"size:100;not null;uniqueIndex:idx_roles_workspace_name,where:deleted_at IS NULL"`

	Permissions []RolePermission `gorm:"foreignKey:RoleID;constraint:OnDelete:CASCADE;"`
}

type RolePermission struct {
	gorm.Model

	RoleID       uint `gorm:"not null;index;uniqueIndex:idx_role_permission,where:deleted_at IS NULL"`
	PermissionID uint `gorm:"not null;index;uniqueIndex:idx_role_permission,where:deleted_at IS NULL"`

	Permission Permission `gorm:"foreignKey:PermissionID;constraint:OnDelete:RESTRICT;"`
}

func (RolePermission) TableName() string { return "role_permissions" }

type Permission struct {
	gorm.Model

	Code        string `gorm:"size:100;not null;uniqueIndex:idx_permissions_code,where:deleted_at IS NULL"`
	Description string `gorm:"size:255"`
}

type Space struct {
	gorm.Model
	Name        string `gorm:"not null"`
	WorkspaceID uint   `gorm:"not null;index;column:workspace_id"`

	Currency string `gorm:"not null;default:'USD'"`
	OwnerID  uint   `gorm:"not null"`

	Lists []List `gorm:"foreignKey:SpaceID;references:ID;constraint:OnDelete:CASCADE"`
	Tasks []Task `gorm:"foreignKey:SpaceID;references:ID;constraint:OnDelete:CASCADE"`
	Tags  []Tag  `gorm:"foreignKey:SpaceID;constraint:OnDelete:CASCADE"`
}

type Folder struct {
	gorm.Model
	Name     string `gorm:"size:255;not null"`
	SpaceID  uint   `gorm:"not null;index"`
	ParentID *uint  `gorm:"index"`
}

type List struct {
	gorm.Model
	Name           string       `gorm:"not null"`
	SpaceID        uint         `gorm:"index;not null;column:space_id"`
	FolderID       *uint        `gorm:"index"`
	Statuses       []TaskStatus `gorm:"foreignKey:ListID;constraint:OnDelete:CASCADE"`
	Tasks          []Task       `gorm:"foreignKey:ListID"`
	ParentStatusID *uint        `gorm:"column:parent_status_id;index"`
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
	gorm.Model
	SpaceID  uint       `gorm:"index;not null;column:space_id"`
	ListID   uint       `gorm:"index;not null;column:list_id"`
	Name     string     `gorm:"not null"`
	Color    string     `gorm:"size:7;not null"`
	Position int        `gorm:"type:integer;not null"`
	Type     StatusType `gorm:"type:task_status_type;not null"`
}

type Tag struct {
	ID      uint   `gorm:"primaryKey"`
	SpaceID uint   `gorm:"index;not null;column:space_id"`
	Name    string `gorm:"not null"`
	Color   string `gorm:"size:7;not null"`
}

type Task struct {
	gorm.Model
	SpaceID      uint `gorm:"index;not null;column:space_id"`
	ListID       uint `gorm:"index;not null;column:list_id"`
	StatusID     uint `gorm:"not null;column:status_id"`
	ParentID     *uint
	Title        string `gorm:"not null"`
	Description  string
	Priority     int    `gorm:"type:integer;not null;default:2"`
	UserID       uint   `gorm:"not null"`
	Assignees    []User `gorm:"many2many:task_assignees;constraint:OnDelete:CASCADE"`
	Tags         []Tag  `gorm:"many2many:task_tags;constraint:OnDelete:CASCADE"`
	StartDate    *time.Time
	DueDate      *time.Time
	TimeEstimate int `gorm:"type:integer;not null;default:0"`
	TimeSpent    int `gorm:"type:integer;not null;default:0"`

	SortStatusPosition int     `gorm:"->;-:migration"`
	SortAssignee       *string `gorm:"->;-:migration"`
}
