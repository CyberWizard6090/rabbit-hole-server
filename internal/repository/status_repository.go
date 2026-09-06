package repository

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type StatusRepository interface {
	Create(status *domain.TaskStatus) error

	GetAllBySpace(
		spaceID uint,
	) ([]domain.TaskStatus, error)

	GetByID(
		statusID uint,
	) (*domain.TaskStatus, error)

	Update(
		status *domain.TaskStatus,
	) error

	Delete(
		spaceID uint,
		statusID uint,
	) error

	ShiftPositions(
		spaceID uint,
		startPosition int,
	) error

	DecrementPositionsAfter(
		spaceID uint,
		position int,
	) error
}

type statusRepository struct {
	db *gorm.DB
}

func NewStatusRepository(
	db *gorm.DB,
) StatusRepository {
	return &statusRepository{
		db: db,
	}
}

func (r *statusRepository) Create(
	status *domain.TaskStatus,
) error {

	return r.db.
		Create(status).
		Error
}

func (r *statusRepository) GetAllBySpace(
	spaceID uint,
) ([]domain.TaskStatus, error) {

	var statuses []domain.TaskStatus

	err := r.db.
		Where("space_id = ?", spaceID).
		Order("position ASC").
		Find(&statuses).
		Error

	return statuses, err
}

func (r *statusRepository) GetByID(
	statusID uint,
) (*domain.TaskStatus, error) {

	var status domain.TaskStatus

	err := r.db.
		First(&status, statusID).
		Error

	if err != nil {
		return nil, err
	}

	return &status, nil
}

func (r *statusRepository) Update(
	status *domain.TaskStatus,
) error {

	return r.db.
		Save(status).
		Error
}

func (r *statusRepository) Delete(
	spaceID uint,
	statusID uint,
) error {

	return r.db.
		Where(
			"id = ? AND space_id = ?",
			statusID,
			spaceID,
		).
		Delete(&domain.TaskStatus{}).
		Error
}

func (r *statusRepository) ShiftPositions(
	spaceID uint,
	startPosition int,
) error {

	return r.db.
		Model(&domain.TaskStatus{}).
		Where(
			"space_id = ? AND position >= ?",
			spaceID,
			startPosition,
		).
		Update(
			"position",
			gorm.Expr("position + 1"),
		).
		Error
}

func (r *statusRepository) DecrementPositionsAfter(
	spaceID uint,
	position int,
) error {

	return r.db.
		Model(&domain.TaskStatus{}).
		Where(
			"space_id = ? AND position > ?",
			spaceID,
			position,
		).
		Update(
			"position",
			gorm.Expr("position - 1"),
		).
		Error
}
