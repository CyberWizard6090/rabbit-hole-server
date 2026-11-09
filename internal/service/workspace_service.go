package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"rabbit-hole-server/internal/domain"
)

const adminRoleName = "Admin"

var (
	ErrWorkspaceNameRequired = errors.New("workspace name is required")
	ErrWorkspaceNotFound     = errors.New("workspace not found")

	ErrInvalidMemberRole    = errors.New("role cannot be assigned")
	ErrMemberNotContact     = errors.New("user must be one of your contacts")
	ErrOwnerImmutable       = errors.New("the workspace owner cannot be changed or removed")
	ErrAdminOwnerOnly       = errors.New("only the workspace owner can grant, change or remove the Admin role")
	ErrMemberTargetNotFound = errors.Join(domain.ErrMemberNotFound, domain.ErrNotFound)
)

type WorkspaceService interface {
	CreateWorkspace(
		ctx context.Context,
		userID uint,
		name string,
		description string,
	) (*domain.Workspace, error)

	GetWorkspaceByID(ctx context.Context, id uint) (*domain.Workspace, error)

	ListForUser(ctx context.Context, userID uint, limit, offset int) ([]domain.WorkspaceSummary, int64, error)

	ListMembers(ctx context.Context, workspaceID uint) ([]domain.MemberDetail, error)
	AddMember(ctx context.Context, workspaceID, actorID, targetID uint, roleName string) error
	ChangeMemberRole(ctx context.Context, workspaceID, actorID, targetID uint, roleName string) error
	RemoveMember(ctx context.Context, workspaceID, actorID, targetID uint) error
	Leave(ctx context.Context, workspaceID, userID uint) error
	UpdateWorkspace(ctx context.Context, id uint, name, description *string) (*domain.Workspace, error)
	DeleteWorkspace(ctx context.Context, id uint) error
}

type WorkspaceAvatarRemover interface {
	RemoveWorkspaceAvatar(ctx context.Context, workspaceID uint) error
}

type workspaceService struct {
	repo        domain.WorkspaceRepository
	revoker     ConnectionRevoker
	avatars     WorkspaceAvatarRemover
	permissions PermissionChecker
}

func NewWorkspaceServiceWithRevoker(repo domain.WorkspaceRepository, revoker ConnectionRevoker, permissions PermissionChecker) WorkspaceService {
	return &workspaceService{repo: repo, revoker: revoker, permissions: permissions}
}

func NewWorkspaceServiceWithAvatars(repo domain.WorkspaceRepository, revoker ConnectionRevoker, permissions PermissionChecker, avatars WorkspaceAvatarRemover) WorkspaceService {
	return &workspaceService{repo: repo, revoker: revoker, avatars: avatars, permissions: permissions}
}

func NewWorkspaceService(
	repo domain.WorkspaceRepository,
	permissions PermissionChecker,
) WorkspaceService {
	return &workspaceService{
		repo:        repo,
		permissions: permissions,
	}
}

func (s *workspaceService) CreateWorkspace(
	ctx context.Context,
	userID uint,
	name string,
	description string,
) (*domain.Workspace, error) {

	name = strings.TrimSpace(name)
	description = strings.TrimSpace(description)

	if name == "" {
		return nil, ErrWorkspaceNameRequired
	}

	if userID == 0 {
		return nil, errors.New("user id is required")
	}

	workspace := &domain.Workspace{
		Name:        name,
		Description: description,
		OwnerID:     userID,
	}

	if err := s.repo.Create(ctx, workspace, userID); err != nil {
		return nil, err
	}

	return workspace, nil
}

func (s *workspaceService) GetWorkspaceByID(
	ctx context.Context,
	id uint,
) (*domain.Workspace, error) {

	workspace, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if workspace == nil {
		return nil, ErrWorkspaceNotFound
	}

	return workspace, nil
}

func (s *workspaceService) ListForUser(
	ctx context.Context,
	userID uint,
	limit, offset int,
) ([]domain.WorkspaceSummary, int64, error) {
	return s.repo.ListForUser(ctx, userID, limit, offset)
}

func (s *workspaceService) ListMembers(ctx context.Context, workspaceID uint) ([]domain.MemberDetail, error) {
	return s.repo.ListMembers(ctx, workspaceID)
}

func (s *workspaceService) assignableRole(ctx context.Context, workspaceID uint, roleName string) (*domain.Role, error) {
	if !slices.Contains(domain.AssignableRoles(), roleName) {
		return nil, ErrInvalidMemberRole
	}
	return s.repo.GetRoleByName(ctx, workspaceID, roleName)
}

func (s *workspaceService) requireAdminManagement(ctx context.Context, workspaceID, actorID uint) error {
	allowed, err := s.permissions.HasWorkspacePermission(ctx, actorID, workspaceID, domain.PermAdminManage)
	if err != nil {
		return err
	}
	if !allowed {
		return ErrAdminOwnerOnly
	}
	return nil
}

func (s *workspaceService) AddMember(ctx context.Context, workspaceID, actorID, targetID uint, roleName string) error {
	role, err := s.assignableRole(ctx, workspaceID, roleName)
	if err != nil {
		return err
	}
	if roleName == adminRoleName {
		if err := s.requireAdminManagement(ctx, workspaceID, actorID); err != nil {
			return err
		}
	}

	isContact, err := s.repo.IsContact(ctx, actorID, targetID)
	if err != nil {
		return err
	}
	if !isContact {
		return ErrMemberNotContact
	}

	return s.repo.AddMember(ctx, &domain.Member{WorkspaceID: workspaceID, UserID: targetID, RoleID: role.ID})
}

func (s *workspaceService) ensureNotOwner(ctx context.Context, workspaceID, targetID uint) error {
	workspace, err := s.repo.GetByID(ctx, workspaceID)
	if err != nil {
		return err
	}
	if workspace.OwnerID == targetID {
		return ErrOwnerImmutable
	}
	return nil
}

func (s *workspaceService) currentRole(ctx context.Context, workspaceID, targetID uint) (string, error) {
	member, err := s.repo.GetMember(ctx, workspaceID, targetID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) || errors.Is(err, domain.ErrMemberNotFound) {
			return "", ErrMemberTargetNotFound
		}
		return "", err
	}
	if member == nil {
		return "", ErrMemberTargetNotFound
	}
	return member.Role.Name, nil
}

func (s *workspaceService) ChangeMemberRole(ctx context.Context, workspaceID, actorID, targetID uint, roleName string) error {
	role, err := s.assignableRole(ctx, workspaceID, roleName)
	if err != nil {
		return err
	}
	if err := s.ensureNotOwner(ctx, workspaceID, targetID); err != nil {
		return err
	}
	current, err := s.currentRole(ctx, workspaceID, targetID)
	if err != nil {
		return err
	}
	if roleName == adminRoleName || current == adminRoleName {
		if err := s.requireAdminManagement(ctx, workspaceID, actorID); err != nil {
			return err
		}
	}
	if err := s.repo.UpdateMemberRole(ctx, workspaceID, targetID, role.ID); err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return ErrMemberTargetNotFound
		}
		return err
	}
	return nil
}

func (s *workspaceService) RemoveMember(ctx context.Context, workspaceID, actorID, targetID uint) error {
	if err := s.ensureNotOwner(ctx, workspaceID, targetID); err != nil {
		return err
	}
	current, err := s.currentRole(ctx, workspaceID, targetID)
	if err != nil {
		return err
	}
	if current == adminRoleName {
		if err := s.requireAdminManagement(ctx, workspaceID, actorID); err != nil {
			return err
		}
	}
	return s.remove(ctx, workspaceID, targetID)
}

func (s *workspaceService) Leave(ctx context.Context, workspaceID, userID uint) error {
	if err := s.ensureNotOwner(ctx, workspaceID, userID); err != nil {
		return err
	}
	return s.remove(ctx, workspaceID, userID)
}

func (s *workspaceService) remove(ctx context.Context, workspaceID, targetID uint) error {
	if err := s.repo.RemoveMember(ctx, workspaceID, targetID); err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return ErrMemberTargetNotFound
		}
		return err
	}
	disconnectUserFeeds(s.revoker, targetID)
	return nil
}

func (s *workspaceService) UpdateWorkspace(ctx context.Context, id uint, name, description *string) (*domain.Workspace, error) {
	workspace, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, ErrWorkspaceNameRequired
		}
		workspace.Name = trimmed
	}
	if description != nil {
		workspace.Description = strings.TrimSpace(*description)
	}

	if err := s.repo.Update(ctx, workspace); err != nil {
		return nil, err
	}
	return workspace, nil
}

func (s *workspaceService) DeleteWorkspace(ctx context.Context, id uint) error {
	var members []domain.MemberDetail
	if s.revoker != nil {
		members, _ = s.repo.ListMembers(ctx, id)
	}
	if err := s.repo.Delete(ctx, id); err != nil {
		return err
	}
	for _, member := range members {
		disconnectUserFeeds(s.revoker, member.UserID)
	}
	if s.avatars != nil {
		if err := s.avatars.RemoveWorkspaceAvatar(ctx, id); err != nil {
			slog.Warn("workspaces: cannot remove the avatar of a deleted workspace", "workspace_id", id, "error", err)
		}
	}
	return nil
}
