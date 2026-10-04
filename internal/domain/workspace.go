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

type WorkspaceRepository interface {
	Create(workspace *Workspace, ownerID uint) error
	GetByID(id uint) (*Workspace, error)
	GetAllForUser(userID uint) ([]Workspace, error)
}
