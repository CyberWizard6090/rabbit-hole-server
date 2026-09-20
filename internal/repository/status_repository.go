package repository

import (
	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

type StatusRepository interface {
	Create(status *domain.TaskStatus) error

	GetAllByList(
		listID uint,
	) ([]domain.TaskStatus, error)

	GetByID(
		statusID uint,
	) (*domain.TaskStatus, error)

	Update(
		status *domain.TaskStatus,
	) error

	UpdatePosition(
		status *domain.TaskStatus,
		position int,
	) error

	Delete(
		listID uint,
		statusID uint,
	) error

	ShiftPositions(
		listID uint,
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

func (r *statusRepository) GetAllByList(
	listID uint,
) ([]domain.TaskStatus, error) {

	var statuses []domain.TaskStatus

	err := r.db.
		Where("list_id = ?", listID).
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

func (r *statusRepository) UpdatePosition(status *domain.TaskStatus, position int) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&domain.TaskStatus{}).Where("list_id = ?", status.ListID).Count(&count).Error; err != nil {
			return err
		}
		if position < 1 {
			position = 1
		}
		if position > int(count) {
			position = int(count)
		}

		if position < status.Position {
			if err := tx.Model(&domain.TaskStatus{}).
				Where("list_id = ? AND position >= ? AND position < ?", status.ListID, position, status.Position).
				Update("position", gorm.Expr("position + 1")).Error; err != nil {
				return err
			}
		} else if position > status.Position {
			if err := tx.Model(&domain.TaskStatus{}).
				Where("list_id = ? AND position > ? AND position <= ?", status.ListID, status.Position, position).
				Update("position", gorm.Expr("position - 1")).Error; err != nil {
				return err
			}
		}

		status.Position = position
		return tx.Save(status).Error
	})
}

func (r *statusRepository) Delete(
	listID uint,
	statusID uint,
) error {

	res := r.db.
		Where(
			"id = ? AND list_id = ?",
			statusID,
			listID,
		).
		Delete(&domain.TaskStatus{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *statusRepository) ShiftPositions(
	listID uint,
	startPosition int,
) error {

	return r.db.
		Model(&domain.TaskStatus{}).
		Where(
			"list_id = ? AND position >= ?",
			listID,
			startPosition,
		).
		Update(
			"position",
			gorm.Expr("position + 1"),
		).
		Error
}

func (r *statusRepository) DecrementPositionsAfter(
	listID uint,
	position int,
) error {

	return r.db.
		Model(&domain.TaskStatus{}).
		Where(
			"list_id = ? AND position > ?",
			listID,
			position,
		).
		Update(
			"position",
			gorm.Expr("position - 1"),
		).
		Error
}
