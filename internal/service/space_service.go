package service

import (
	"context"
	"errors"
	"strings"

	"rabbit-hole-server/internal/domain"
)

var ErrSpaceNameRequired = errors.New("space name is required")

type SpaceService interface {
	CreateSpace(ctx context.Context, workspaceID uint, name string, currency string, userID uint) (*domain.Space, error)
	GetSpaceByID(ctx context.Context, id uint) (*domain.Space, error)
	GetAllSpaces(ctx context.Context, workspaceID uint, limit, offset int) ([]domain.Space, int64, error)
	GetDashboard(ctx context.Context, spaceID uint) (*domain.Space, error)
	UpdateSpace(ctx context.Context, id uint, name, currency *string) (*domain.Space, error)
	DeleteSpace(ctx context.Context, id uint) error
	CreateList(ctx context.Context, spaceID uint, name string, parentStatusID *uint) (*domain.List, error)

	CreateListInFolder(ctx context.Context, spaceID, folderID uint, name string) (*domain.List, error)
	GetListsByFolder(ctx context.Context, folderID uint) ([]domain.List, error)
	UpdateList(ctx context.Context, listID uint, name *string) (*domain.List, error)
	DeleteList(ctx context.Context, listID uint) error
}

type spaceService struct {
	repo domain.SpaceRepository
}

func NewSpaceService(repo domain.SpaceRepository) SpaceService {
	return &spaceService{repo: repo}
}

func (s *spaceService) CreateSpace(ctx context.Context, workspaceID uint, name string, currency string, userID uint) (*domain.Space, error) {
	space := &domain.Space{
		WorkspaceID: workspaceID,
		Name:        name,
		Currency:    currency,
		OwnerID:     userID,
	}

	defaultList := &domain.List{Name: "Основной список"}
	defaultStatuses := []domain.TaskStatus{
		{Name: "К выполнению", Color: "#94A3B8", Position: 1, Type: domain.StatusTodo},
		{Name: "В работе", Color: "#3B82F6", Position: 2, Type: domain.StatusInProgress},
		{Name: "Готово", Color: "#22C55E", Position: 3, Type: domain.StatusDone},
	}

	if err := s.repo.CreateWithDefaults(ctx, space, defaultList, defaultStatuses); err != nil {
		return nil, err
	}

	return space, nil
}

func (s *spaceService) GetSpaceByID(ctx context.Context, id uint) (*domain.Space, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *spaceService) GetAllSpaces(ctx context.Context, workspaceID uint, limit, offset int) ([]domain.Space, int64, error) {
	return s.repo.GetAll(ctx, workspaceID, limit, offset)
}

func (s *spaceService) GetDashboard(ctx context.Context, spaceID uint) (*domain.Space, error) {
	return s.repo.GetDashboard(ctx, spaceID)
}

func (s *spaceService) CreateList(ctx context.Context, spaceID uint, name string, parentStatusID *uint) (*domain.List, error) {
	list := &domain.List{SpaceID: spaceID, Name: name, ParentStatusID: parentStatusID}
	if err := s.repo.CreateList(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *spaceService) CreateListInFolder(ctx context.Context, spaceID, folderID uint, name string) (*domain.List, error) {
	list := &domain.List{SpaceID: spaceID, FolderID: &folderID, Name: name}
	if err := s.repo.CreateList(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *spaceService) GetListsByFolder(ctx context.Context, folderID uint) ([]domain.List, error) {
	return s.repo.GetListsByFolder(ctx, folderID)
}

func (s *spaceService) UpdateList(ctx context.Context, listID uint, name *string) (*domain.List, error) {
	list, err := s.repo.GetListByID(ctx, listID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		list.Name = *name
	}
	if err := s.repo.UpdateList(ctx, list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *spaceService) DeleteList(ctx context.Context, listID uint) error {
	return s.repo.DeleteList(ctx, listID)
}

func (s *spaceService) UpdateSpace(ctx context.Context, id uint, name, currency *string) (*domain.Space, error) {
	space, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if name != nil {
		trimmed := strings.TrimSpace(*name)
		if trimmed == "" {
			return nil, ErrSpaceNameRequired
		}
		space.Name = trimmed
	}
	if currency != nil {
		space.Currency = strings.ToUpper(strings.TrimSpace(*currency))
	}

	if err := s.repo.Update(ctx, space); err != nil {
		return nil, err
	}
	return space, nil
}

func (s *spaceService) DeleteSpace(ctx context.Context, id uint) error {
	return s.repo.Delete(ctx, id)
}
