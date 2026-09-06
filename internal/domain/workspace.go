package domain

import (
	"gorm.io/gorm"
)

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

	WorkspaceID uint `gorm:"not null;index;uniqueIndex:idx_workspace_user"`
	UserID      uint `gorm:"not null;index;uniqueIndex:idx_workspace_user"`
	RoleID      uint `gorm:"not null;index"`

	Role Role `gorm:"foreignKey:RoleID;constraint:OnDelete:RESTRICT;"`
}

func (Member) TableName() string { return "workspace_members" }

type Role struct {
	gorm.Model

	WorkspaceID uint   `gorm:"not null;index"`
	Name        string `gorm:"size:100;not null"`

	Permissions []RolePermission `gorm:"foreignKey:RoleID;constraint:OnDelete:CASCADE;"`
}
type RolePermission struct {
	gorm.Model

	RoleID       uint `gorm:"not null;index;uniqueIndex:idx_role_permission"`
	PermissionID uint `gorm:"not null;index;uniqueIndex:idx_role_permission"`

	Permission Permission `gorm:"foreignKey:PermissionID;constraint:OnDelete:CASCADE;"`
}

func (RolePermission) TableName() string { return "role_permissions" }

type Permission struct {
	gorm.Model

	Code        string `gorm:"size:100;not null;uniqueIndex"`
	Description string `gorm:"size:255"`
}

type WorkspaceRepository interface {
	Create(workspace *Workspace, ownerID uint) error
	GetByID(id uint) (*Workspace, error)
	GetAllForUser(userID uint) ([]Workspace, error)
}
