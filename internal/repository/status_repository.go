package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
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
	m := statusFromDomain(*status)
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var list persistence.List
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&list, m.ListID).Error; err != nil {
			return err
		}

		var count int64
		if err := tx.Model(&persistence.TaskStatus{}).Where(statusListPredicate, m.ListID).Count(&count).Error; err != nil {
			return err
		}
		position := m.Position
		if position < 1 {
			position = 1
		}
		if position > int(count)+1 {
			position = int(count) + 1
		}
		if position <= int(count) {
			if err := tx.Model(&persistence.TaskStatus{}).
				Where(statusListPredicate+" AND position >= ?", m.ListID, position).
				Update("position", gorm.Expr(statusPositionIncrement)).Error; err != nil {
				return err
			}
		}
		m.Position = position
		return tx.Create(&m).Error
	})
	if err != nil {
		return mapErr(err)
	}
	*status = statusToDomain(m)
	return nil
}

func (r *statusRepository) GetAllByList(ctx context.Context, listID uint) ([]domain.TaskStatus, error) {
	var rows []persistence.TaskStatus

	err := database.DBFromContext(ctx, r.db).
		Where("list_id = ?", listID).
		Order("position ASC").
		Find(&rows).
		Error
	if err != nil {
		return nil, mapErr(err)
	}

	return mapSlice(rows, statusToDomain), nil
}

func (r *statusRepository) GetByID(ctx context.Context, statusID uint) (*domain.TaskStatus, error) {
	var m persistence.TaskStatus

	if err := database.DBFromContext(ctx, r.db).First(&m, statusID).Error; err != nil {
		return nil, mapErr(err)
	}

	status := statusToDomain(m)
	return &status, nil
}

func (r *statusRepository) Update(ctx context.Context, status *domain.TaskStatus) error {
	m := statusFromDomain(*status)
	res := database.DBFromContext(ctx, r.db).Model(&m).Select("Name", "Color", "Type").Updates(&m)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	status.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *statusRepository) UpdatePosition(ctx context.Context, status *domain.TaskStatus, position int) error {
	m := statusFromDomain(*status)
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var list persistence.List
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&list, m.ListID).Error; err != nil {
			return err
		}
		var current persistence.TaskStatus
		if err := tx.Where(statusListPredicate, m.ListID).First(&current, m.ID).Error; err != nil {
			return err
		}
		m.Position = current.Position

		var count int64
		if err := tx.Model(&persistence.TaskStatus{}).Where(statusListPredicate, m.ListID).Count(&count).Error; err != nil {
			return err
		}
		if position < 1 {
			position = 1
		}
		if position > int(count) {
			position = int(count)
		}

		if position < m.Position {
			if err := tx.Model(&persistence.TaskStatus{}).
				Where("list_id = ? AND position >= ? AND position < ?", m.ListID, position, m.Position).
				Update("position", gorm.Expr(statusPositionIncrement)).Error; err != nil {
				return err
			}
		} else if position > m.Position {
			if err := tx.Model(&persistence.TaskStatus{}).
				Where("list_id = ? AND position > ? AND position <= ?", m.ListID, m.Position, position).
				Update("position", gorm.Expr("position - 1")).Error; err != nil {
				return err
			}
		}

		m.Position = position
		return tx.Model(&m).Select("Name", "Color", "Type", "Position").Updates(&m).Error
	})
	if err != nil {
		status.Position = m.Position
		return mapErr(err)
	}
	status.Position = m.Position
	status.UpdatedAt = m.UpdatedAt
	return nil
}

func (r *statusRepository) Delete(ctx context.Context, listID uint, statusID uint, replacementID *uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var list persistence.List
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&list, listID).Error; err != nil {
			return err
		}

		var status persistence.TaskStatus
		if err := tx.Where(statusListPredicate, listID).First(&status, statusID).Error; err != nil {
			return err
		}

		formIDs, err := publishedFormsUsingStatus(tx, statusID)
		if err != nil {
			return err
		}
		if len(formIDs) > 0 {
			return &domain.StatusUsedByFormError{FormIDs: formIDs}
		}

		var replacement *persistence.TaskStatus
		if replacementID != nil {
			if *replacementID == statusID {
				return domain.ErrInvalidReplacementStatus
			}
			var found persistence.TaskStatus
			if err := tx.Where(statusListPredicate, listID).First(&found, *replacementID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return domain.ErrInvalidReplacementStatus
				}
				return err
			}
			replacement = &found
		}

		var inUse int64
		if err := tx.Model(&persistence.Task{}).Where("status_id = ?", statusID).Count(&inUse).Error; err != nil {
			return err
		}
		if inUse > 0 && replacement == nil {
			return domain.ErrStatusInUse
		}
		if replacement != nil {
			if err := tx.Unscoped().Model(&persistence.Task{}).
				Where("list_id = ? AND status_id = ?", listID, statusID).
				Update("status_id", replacement.ID).Error; err != nil {
				return err
			}
		}

		if err := tx.Delete(&status).Error; err != nil {
			return err
		}

		return tx.Model(&persistence.TaskStatus{}).
			Where(statusListPredicate+" AND position > ?", listID, status.Position).
			Update("position", gorm.Expr("position - 1")).Error
	}))
}

const publishedFormsUsingStatusSQL = `
SELECT DISTINCT f.id
FROM forms f
JOIN form_task_configs c ON c.form_id = f.id AND c.form_version_id = f.current_version_id
WHERE f.status = 'published' AND c.enabled
  AND (
    c.configuration->'task_defaults'->>'status_id' = @status
    OR EXISTS (
      SELECT 1
      FROM jsonb_array_elements(COALESCE(c.configuration->'mappings', '[]'::jsonb)) m,
           jsonb_each_text(COALESCE(m->'transform'->'values', '{}'::jsonb)) kv
      WHERE m->'target'->>'field' = 'status_id' AND kv.value = @status
    )
    OR EXISTS (
      SELECT 1 FROM task_template_versions v
      WHERE v.id = c.template_version_id AND v.configuration->'task'->>'status_id' = @status
    )
  )
ORDER BY f.id`

func publishedFormsUsingStatus(tx *gorm.DB, statusID uint) ([]uint, error) {
	var ids []uint
	err := tx.Raw(publishedFormsUsingStatusSQL, sql.Named("status", strconv.FormatUint(uint64(statusID), 10))).
		Scan(&ids).Error
	return ids, err
}
