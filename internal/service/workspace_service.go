package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"

	"rabbit-hole-server/internal/domain"
)

var (
	ErrWorkspaceNameRequired = errors.New("workspace name is required")
	ErrWorkspaceNotFound     = errors.New("workspace not found")

	ErrInvalidMemberRole    = errors.New("role cannot be assigned")
	ErrMemberNotContact     = errors.New("user must be one of your contacts")
	ErrOwnerImmutable       = errors.New("the workspace owner cannot be changed or removed")
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

	GetAllForUser(ctx context.Context, userID uint) ([]domain.Workspace, error)

	ListMembers(ctx context.Context, workspaceID uint) ([]domain.MemberDetail, error)
	AddMember(ctx context.Context, workspaceID, actorID, targetID uint, roleName string) error
	ChangeMemberRole(ctx context.Context, workspaceID, targetID uint, roleName string) error
	RemoveMember(ctx context.Context, workspaceID, targetID uint) error
	UpdateWorkspace(ctx context.Context, id uint, name, description *string) (*domain.Workspace, error)
	DeleteWorkspace(ctx context.Context, id uint) error
}

type WorkspaceAvatarRemover interface {
	RemoveWorkspaceAvatar(ctx context.Context, workspaceID uint) error
}

type workspaceService struct {
	repo    domain.WorkspaceRepository
	revoker ConnectionRevoker
	avatars WorkspaceAvatarRemover
}

func NewWorkspaceServiceWithRevoker(repo domain.WorkspaceRepository, revoker ConnectionRevoker) WorkspaceService {
	return &workspaceService{repo: repo, revoker: revoker}
}

func NewWorkspaceServiceWithAvatars(repo domain.WorkspaceRepository, revoker ConnectionRevoker, avatars WorkspaceAvatarRemover) WorkspaceService {
	return &workspaceService{repo: repo, revoker: revoker, avatars: avatars}
}

func NewWorkspaceService(
	repo domain.WorkspaceRepository,
) WorkspaceService {
	return &workspaceService{
		repo: repo,
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

func (s *workspaceService) GetAllForUser(
	ctx context.Context,
	userID uint,
) ([]domain.Workspace, error) {
	return s.repo.GetAllForUser(ctx, userID)
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

func (s *workspaceService) AddMember(ctx context.Context, workspaceID, actorID, targetID uint, roleName string) error {
	role, err := s.assignableRole(ctx, workspaceID, roleName)
	if err != nil {
		return err
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

func (s *workspaceService) ChangeMemberRole(ctx context.Context, workspaceID, targetID uint, roleName string) error {
	role, err := s.assignableRole(ctx, workspaceID, roleName)
	if err != nil {
		return err
	}
	if err := s.ensureNotOwner(ctx, workspaceID, targetID); err != nil {
		return err
	}
	if err := s.repo.UpdateMemberRole(ctx, workspaceID, targetID, role.ID); err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return ErrMemberTargetNotFound
		}
		return err
	}
	return nil
}

func (s *workspaceService) RemoveMember(ctx context.Context, workspaceID, targetID uint) error {
	if err := s.ensureNotOwner(ctx, workspaceID, targetID); err != nil {
		return err
	}
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
