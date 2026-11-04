package service

import (
	"context"
	"fmt"

	"rabbit-hole-server/internal/domain"
)

// ErrStatusListMismatch is returned when a status is addressed through a list
// it does not belong to. It wraps domain.ErrNotFound so the API answers 404.
var ErrStatusListMismatch = fmt.Errorf("status does not belong to this list: %w", domain.ErrNotFound)

type StatusService interface {
	Create(ctx context.Context, params CreateStatusParams) (*domain.TaskStatus, error)
	GetAllByList(ctx context.Context, listID uint) ([]domain.TaskStatus, error)
	Update(ctx context.Context, listID uint, statusID uint, params UpdateStatusParams) (*domain.TaskStatus, error)
	Delete(ctx context.Context, listID uint, statusID uint, replacementID *uint) error
}

type CreateStatusParams struct {
	ListID uint

	Name     string
	Color    string
	Position *int
	Type     int
}

type UpdateStatusParams struct {
	Name     *string
	Color    *string
	Position *int
	Type     *int
}

type statusService struct {
	repo      domain.StatusRepository
	spaceRepo domain.SpaceRepository
}

func NewStatusService(repo domain.StatusRepository, spaceRepo domain.SpaceRepository) StatusService {
	return &statusService{repo: repo, spaceRepo: spaceRepo}
}

func (s *statusService) Create(ctx context.Context, params CreateStatusParams) (*domain.TaskStatus, error) {
	list, err := s.spaceRepo.GetListByID(ctx, params.ListID)
	if err != nil {
		return nil, fmt.Errorf("get list by id: %w", err)
	}

	existingStatuses, err := s.repo.GetAllByList(ctx, params.ListID)
	if err != nil {
		return nil, err
	}

	position := len(existingStatuses) + 1
	if params.Position != nil {
		position = *params.Position
		if position < 1 {
			position = 1
		}
		if position > len(existingStatuses)+1 {
			position = len(existingStatuses) + 1
		}
	}

	status := &domain.TaskStatus{
		SpaceID:  list.SpaceID,
		ListID:   params.ListID,
		Name:     params.Name,
		Color:    params.Color,
		Position: position,
		Type:     domain.StatusType(params.Type),
	}

	if err := s.repo.Create(ctx, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (s *statusService) GetAllByList(ctx context.Context, listID uint) ([]domain.TaskStatus, error) {
	_, err := s.spaceRepo.GetListByID(ctx, listID)
	if err != nil {
		return nil, fmt.Errorf("get list by id: %w", err)
	}
	return s.repo.GetAllByList(ctx, listID)
}

func (s *statusService) Update(ctx context.Context, listID uint, statusID uint, params UpdateStatusParams) (*domain.TaskStatus, error) {
	status, err := s.repo.GetByID(ctx, statusID)
	if err != nil {
		return nil, err
	}
	if status.ListID != listID {
		return nil, ErrStatusListMismatch
	}

	if params.Name != nil {
		status.Name = *params.Name
	}
	if params.Color != nil {
		status.Color = *params.Color
	}
	if params.Type != nil {
		status.Type = domain.StatusType(*params.Type)
	}
	if params.Position != nil {
		if err := s.repo.UpdatePosition(ctx, status, *params.Position); err != nil {
			return nil, err
		}
		return status, nil
	}

	if err := s.repo.Update(ctx, status); err != nil {
		return nil, err
	}
	return status, nil
}

func (s *statusService) Delete(ctx context.Context, listID uint, statusID uint, replacementID *uint) error {
	status, err := s.repo.GetByID(ctx, statusID)
	if err != nil {
		return err
	}
	if status.ListID != listID {
		return ErrStatusListMismatch
	}

	return s.repo.Delete(ctx, listID, statusID, replacementID)
}
