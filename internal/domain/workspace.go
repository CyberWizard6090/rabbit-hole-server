package domain

import (
	"errors"
	"time"
)

type Workspace struct {
	Model
	Name        string `gorm:"size:255;not null" json:"name"`
	OwnerID     uint   `gorm:"not null" json:"owner_id"`
	Description string `gorm:"type:text" json:"description"`

	Members []Member `gorm:"foreignKey:WorkspaceID;constraint:OnDelete:CASCADE;" json:"-"`
	Spaces  []Space  `gorm:"foreignKey:WorkspaceID;constraint:OnDelete:CASCADE;" json:"-"`
	Roles   []Role   `gorm:"foreignKey:WorkspaceID;constraint:OnDelete:CASCADE;" json:"-"`
}

type Member struct {
	Model

	WorkspaceID uint `gorm:"not null;index;uniqueIndex:idx_workspace_user,where:deleted_at IS NULL"`
	UserID      uint `gorm:"not null;index;uniqueIndex:idx_workspace_user,where:deleted_at IS NULL"`
	RoleID      uint `gorm:"not null;index"`

	Role Role `gorm:"foreignKey:RoleID;constraint:OnDelete:RESTRICT;"`
}

func (Member) TableName() string { return "workspace_members" }

type Role struct {
	Model

	WorkspaceID uint   `gorm:"not null;index;uniqueIndex:idx_roles_workspace_name,where:deleted_at IS NULL"`
	Name        string `gorm:"size:100;not null;uniqueIndex:idx_roles_workspace_name,where:deleted_at IS NULL"`

	Permissions []RolePermission `gorm:"foreignKey:RoleID;constraint:OnDelete:CASCADE;"`
}
type RolePermission struct {
	Model

	RoleID       uint `gorm:"not null;index;uniqueIndex:idx_role_permission,where:deleted_at IS NULL"`
	PermissionID uint `gorm:"not null;index;uniqueIndex:idx_role_permission,where:deleted_at IS NULL"`

	Permission Permission `gorm:"foreignKey:PermissionID;constraint:OnDelete:RESTRICT;"`
}

func (RolePermission) TableName() string { return "role_permissions" }

type Permission struct {
	Model

	Code        string `gorm:"size:100;not null;uniqueIndex:idx_permissions_code,where:deleted_at IS NULL"`
	Description string `gorm:"size:255"`
}

var (
	ErrAlreadyMember  = errors.New("user is already a member of this workspace")
	ErrMemberNotFound = errors.New("member not found")
)

type MemberDetail struct {
	UserID    uint
	Username  string
	FirstName string
	LastName  string
	AvatarURL string
	RoleID    uint
	Role      string
	JoinedAt  time.Time
}

type WorkspaceRepository interface {
	Create(workspace *Workspace, ownerID uint) error
	GetByID(id uint) (*Workspace, error)
	GetAllForUser(userID uint) ([]Workspace, error)

	ListMembers(workspaceID uint) ([]MemberDetail, error)
	GetMember(workspaceID, userID uint) (*Member, error)
	GetRoleByName(workspaceID uint, name string) (*Role, error)
	AddMember(member *Member) error
	UpdateMemberRole(workspaceID, userID, roleID uint) error
	RemoveMember(workspaceID, userID uint) error
	IsContact(userID, otherID uint) (bool, error)
	Update(workspace *Workspace) error
	Delete(id uint) error
}
