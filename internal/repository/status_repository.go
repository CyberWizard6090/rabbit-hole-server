package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rabbit-hole-server/internal/domain"
)

type statusRepository struct {
	db *gorm.DB
}

const (
	statusListPredicate     = "list_id = ?"
	statusPositionIncrement = "position + 1"
)

func NewStatusRepository(db *gorm.DB) domain.StatusRepository {
	return &statusRepository{db: db}
}

func (r *statusRepository) Create(ctx context.Context, status *domain.TaskStatus) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var list domain.List
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&list, status.ListID).Error; err != nil {
			return err
		}

		var count int64
		if err := tx.Model(&domain.TaskStatus{}).Where(statusListPredicate, status.ListID).Count(&count).Error; err != nil {
			return err
		}
		position := status.Position
		if position < 1 {
			position = 1
		}
		if position > int(count)+1 {
			position = int(count) + 1
		}
		if position <= int(count) {
			if err := tx.Model(&domain.TaskStatus{}).
				Where(statusListPredicate+" AND position >= ?", status.ListID, position).
				Update("position", gorm.Expr(statusPositionIncrement)).Error; err != nil {
				return err
			}
		}
		status.Position = position
		return tx.Create(status).Error
	})
}

func (r *statusRepository) GetAllByList(ctx context.Context, listID uint) ([]domain.TaskStatus, error) {

	var statuses []domain.TaskStatus

	err := r.db.WithContext(ctx).
		Where("list_id = ?", listID).
		Order("position ASC").
		Find(&statuses).
		Error

	return statuses, err
}

func (r *statusRepository) GetByID(ctx context.Context, statusID uint) (*domain.TaskStatus, error) {

	var status domain.TaskStatus

	err := r.db.WithContext(ctx).
		First(&status, statusID).
		Error

	if err != nil {
		return nil, err
	}

	return &status, nil
}

func (r *statusRepository) Update(ctx context.Context, status *domain.TaskStatus) error {

	return r.db.WithContext(ctx).
		Save(status).
		Error
}

func (r *statusRepository) UpdatePosition(ctx context.Context, status *domain.TaskStatus, position int) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var list domain.List
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&list, status.ListID).Error; err != nil {
			return err
		}
		var current domain.TaskStatus
		if err := tx.Where(statusListPredicate, status.ListID).First(&current, status.ID).Error; err != nil {
			return err
		}
		status.Position = current.Position

		var count int64
		if err := tx.Model(&domain.TaskStatus{}).Where(statusListPredicate, status.ListID).Count(&count).Error; err != nil {
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
				Update("position", gorm.Expr(statusPositionIncrement)).Error; err != nil {
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

func (r *statusRepository) Delete(ctx context.Context, listID uint, statusID uint, replacementID *uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var list domain.List
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&list, listID).Error; err != nil {
			return err
		}

		var status domain.TaskStatus
		if err := tx.Where(statusListPredicate, listID).First(&status, statusID).Error; err != nil {
			return err
		}

		var inUse int64
		if err := tx.Model(&domain.Task{}).Where("status_id = ?", statusID).Count(&inUse).Error; err != nil {
			return err
		}
		if inUse > 0 {
			if replacementID == nil {
				return domain.ErrStatusInUse
			}
			if *replacementID == statusID {
				return domain.ErrInvalidReplacementStatus
			}
			var replacement domain.TaskStatus
			if err := tx.Where(statusListPredicate, listID).First(&replacement, *replacementID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return domain.ErrInvalidReplacementStatus
				}
				return err
			}
			if err := tx.Model(&domain.Task{}).Where("status_id = ?", statusID).
				Update("status_id", replacement.ID).Error; err != nil {
				return err
			}
		}

		if err := tx.Delete(&status).Error; err != nil {
			return err
		}

		return tx.Model(&domain.TaskStatus{}).
			Where(statusListPredicate+" AND position > ?", listID, status.Position).
			Update("position", gorm.Expr("position - 1")).Error
	})
}
