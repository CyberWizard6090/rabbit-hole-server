package service

import (
	"errors"
	"slices"
	"strings"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

var (
	ErrWorkspaceNameRequired = errors.New("workspace name is required")
	ErrWorkspaceNotFound     = errors.New("workspace not found")

	ErrInvalidMemberRole    = errors.New("role cannot be assigned")
	ErrMemberNotContact     = errors.New("user must be one of your contacts")
	ErrOwnerImmutable       = errors.New("the workspace owner cannot be changed or removed")
	ErrMemberTargetNotFound = errors.Join(domain.ErrMemberNotFound, gorm.ErrRecordNotFound)
)

type WorkspaceService interface {
	CreateWorkspace(
		userID uint,
		name string,
		description string,
	) (*domain.Workspace, error)

	GetWorkspaceByID(id uint) (*domain.Workspace, error)

	GetAllForUser(userID uint) ([]domain.Workspace, error)

	ListMembers(workspaceID uint) ([]domain.MemberDetail, error)
	AddMember(workspaceID, actorID, targetID uint, roleName string) error
	ChangeMemberRole(workspaceID, targetID uint, roleName string) error
	RemoveMember(workspaceID, targetID uint) error
	UpdateWorkspace(id uint, name, description *string) (*domain.Workspace, error)
	DeleteWorkspace(id uint) error
}

type workspaceService struct {
	repo domain.WorkspaceRepository
}

func NewWorkspaceService(
	repo domain.WorkspaceRepository,
) WorkspaceService {
	return &workspaceService{
		repo: repo,
	}
}

func (s *workspaceService) CreateWorkspace(
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

	if err := s.repo.Create(workspace, userID); err != nil {
		return nil, err
	}

	return workspace, nil
}

func (s *workspaceService) GetWorkspaceByID(
	id uint,
) (*domain.Workspace, error) {

	workspace, err := s.repo.GetByID(id)
	if err != nil {
		return nil, err
	}

	if workspace == nil {
		return nil, ErrWorkspaceNotFound
	}

	return workspace, nil
}

func (s *workspaceService) GetAllForUser(
	userID uint,
) ([]domain.Workspace, error) {
	return s.repo.GetAllForUser(userID)
}

func (s *workspaceService) ListMembers(workspaceID uint) ([]domain.MemberDetail, error) {
	return s.repo.ListMembers(workspaceID)
}

func (s *workspaceService) assignableRole(workspaceID uint, roleName string) (*domain.Role, error) {
	if !slices.Contains(domain.AssignableRoles(), roleName) {
		return nil, ErrInvalidMemberRole
	}
	return s.repo.GetRoleByName(workspaceID, roleName)
}

func (s *workspaceService) AddMember(workspaceID, actorID, targetID uint, roleName string) error {
	role, err := s.assignableRole(workspaceID, roleName)
	if err != nil {
		return err
	}

	isContact, err := s.repo.IsContact(actorID, targetID)
	if err != nil {
		return err
	}
	if !isContact {
		return ErrMemberNotContact
	}

	return s.repo.AddMember(&domain.Member{WorkspaceID: workspaceID, UserID: targetID, RoleID: role.ID})
}

func (s *workspaceService) ensureNotOwner(workspaceID, targetID uint) error {
	workspace, err := s.repo.GetByID(workspaceID)
	if err != nil {
		return err
	}
	if workspace.OwnerID == targetID {
		return ErrOwnerImmutable
	}
	return nil
}

func (s *workspaceService) ChangeMemberRole(workspaceID, targetID uint, roleName string) error {
	role, err := s.assignableRole(workspaceID, roleName)
	if err != nil {
		return err
	}
	if err := s.ensureNotOwner(workspaceID, targetID); err != nil {
		return err
	}
	if err := s.repo.UpdateMemberRole(workspaceID, targetID, role.ID); err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return ErrMemberTargetNotFound
		}
		return err
	}
	return nil
}

func (s *workspaceService) RemoveMember(workspaceID, targetID uint) error {
	if err := s.ensureNotOwner(workspaceID, targetID); err != nil {
		return err
	}
	if err := s.repo.RemoveMember(workspaceID, targetID); err != nil {
		if errors.Is(err, domain.ErrMemberNotFound) {
			return ErrMemberTargetNotFound
		}
		return err
	}
	return nil
}

func (s *workspaceService) UpdateWorkspace(id uint, name, description *string) (*domain.Workspace, error) {
	workspace, err := s.repo.GetByID(id)
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

	if err := s.repo.Update(workspace); err != nil {
		return nil, err
	}
	return workspace, nil
}

func (s *workspaceService) DeleteWorkspace(id uint) error {
	return s.repo.Delete(id)
}
