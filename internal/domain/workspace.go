package domain

import (
	"context"
	"errors"
	"time"
)

type Workspace struct {
	Model
	Name        string `json:"name"`
	OwnerID     uint   `json:"owner_id"`
	Description string `json:"description"`

	Members []Member `json:"-"`
	Spaces  []Space  `json:"-"`
	Roles   []Role   `json:"-"`
}

type Member struct {
	Model

	WorkspaceID uint
	UserID      uint
	RoleID      uint

	Role Role
}

type Role struct {
	Model

	WorkspaceID uint
	Name        string

	Permissions []RolePermission
}
type RolePermission struct {
	Model

	RoleID       uint
	PermissionID uint

	Permission Permission
}

type Permission struct {
	Model

	Code        string
	Description string
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
	Create(ctx context.Context, workspace *Workspace, ownerID uint) error
	GetByID(ctx context.Context, id uint) (*Workspace, error)
	GetAllForUser(ctx context.Context, userID uint) ([]Workspace, error)

	ListMembers(ctx context.Context, workspaceID uint) ([]MemberDetail, error)
	GetMember(ctx context.Context, workspaceID, userID uint) (*Member, error)
	GetRoleByName(ctx context.Context, workspaceID uint, name string) (*Role, error)
	AddMember(ctx context.Context, member *Member) error
	UpdateMemberRole(ctx context.Context, workspaceID, userID, roleID uint) error
	RemoveMember(ctx context.Context, workspaceID, userID uint) error
	IsContact(ctx context.Context, userID, otherID uint) (bool, error)
	Update(ctx context.Context, workspace *Workspace) error
	Delete(ctx context.Context, id uint) error
}
