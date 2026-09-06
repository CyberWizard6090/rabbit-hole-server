package service

import (
	"fmt"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/repository"
)

type StatusService interface {
	Create(params CreateStatusParams) (*domain.TaskStatus, error)
	GetAllByList(listID uint) ([]domain.TaskStatus, error)
	Update(listID uint, statusID uint, params UpdateStatusParams) (*domain.TaskStatus, error)
	Delete(listID uint, statusID uint) error
}

type CreateStatusParams struct {
	ListID uint

	Name     string
	Color    string
	Position int
	Type     int
}

type UpdateStatusParams struct {
	Name  *string
	Color *string
	Type  *int
}

type statusService struct {
	repo      repository.StatusRepository
	spaceRepo domain.SpaceRepository
}

func NewStatusService(repo repository.StatusRepository, spaceRepo domain.SpaceRepository) StatusService {
	return &statusService{repo: repo, spaceRepo: spaceRepo}
}

func (s *statusService) Create(params CreateStatusParams) (*domain.TaskStatus, error) {
	list, err := s.spaceRepo.GetListByID(params.ListID)
	if err != nil {
		return nil, fmt.Errorf("get list by id: %w", err)
	}

	existingStatuses, err := s.repo.GetAllBySpace(list.SpaceID)
	if err != nil {
		return nil, err
	}

	position := params.Position
	if position < 0 {
		position = 0
	}
	if position > len(existingStatuses) {
		position = len(existingStatuses)
	}

	if len(existingStatuses) > 0 && position < len(existingStatuses) {
		if err := s.repo.ShiftPositions(list.SpaceID, position); err != nil {
			return nil, err
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

	if err := s.repo.Create(status); err != nil {
		return nil, err
	}
	return status, nil
}

func (s *statusService) GetAllByList(listID uint) ([]domain.TaskStatus, error) {
	list, err := s.spaceRepo.GetListByID(listID)
	if err != nil {
		return nil, fmt.Errorf("get list by id: %w", err)
	}
	return s.repo.GetAllBySpace(list.SpaceID)
}

func (s *statusService) Update(listID uint, statusID uint, params UpdateStatusParams) (*domain.TaskStatus, error) {
	status, err := s.repo.GetByID(statusID)
	if err != nil {
		return nil, err
	}
	if status.ListID != listID {
		return nil, fmt.Errorf("status does not belong to this list")
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

	if err := s.repo.Update(status); err != nil {
		return nil, err
	}
	return status, nil
}

func (s *statusService) Delete(listID uint, statusID uint) error {
	status, err := s.repo.GetByID(statusID)
	if err != nil {
		return err
	}
	if status.ListID != listID {
		return fmt.Errorf("status does not belong to this list")
	}

	if err := s.repo.Delete(status.SpaceID, statusID); err != nil {
		return err
	}
	return s.repo.DecrementPositionsAfter(status.SpaceID, status.Position)
}
