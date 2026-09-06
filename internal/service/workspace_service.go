package service

import (
	"errors"
	"strings"

	"rabbit-hole-server/internal/domain"
)

var (
	ErrWorkspaceNameRequired = errors.New("workspace name is required")
	ErrWorkspaceNotFound     = errors.New("workspace not found")
)

type WorkspaceService interface {
	CreateWorkspace(
		userID uint,
		name string,
		description string,
	) (*domain.Workspace, error)

	GetWorkspaceByID(id uint) (*domain.Workspace, error)

	GetAllForUser(userID uint) ([]domain.Workspace, error)
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
