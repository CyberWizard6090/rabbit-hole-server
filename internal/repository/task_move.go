package repository

import (
	"context"
	"errors"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/persistence"
)

const positionStep int64 = 65536

func lockLists(tx *gorm.DB, ids ...uint) error {
	unique := uniqueIDs(ids)
	sort.Slice(unique, func(i, j int) bool { return unique[i] < unique[j] })
	var rows []persistence.List
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id IN ?", unique).Order("id").Find(&rows).Error
}

func lastPosition(tx *gorm.DB, listID, excludeID uint) (int64, error) {
	var last int64
	err := tx.Raw(
		"SELECT COALESCE(MAX(position), 0) FROM tasks WHERE list_id = ? AND id <> ? AND deleted_at IS NULL",
		listID, excludeID,
	).Scan(&last).Error
	return last, err
}

type anchorRow struct {
	ID       uint
	Position int64
}

func loadAnchor(tx *gorm.DB, listID, taskID, anchorID uint) (anchorRow, error) {
	var rows []anchorRow
	err := tx.Raw(
		"SELECT id, position FROM tasks WHERE id = ? AND list_id = ? AND id <> ? AND deleted_at IS NULL",
		anchorID, listID, taskID,
	).Scan(&rows).Error
	if err != nil {
		return anchorRow{}, err
	}
	if len(rows) == 0 {
		return anchorRow{}, domain.ErrInvalidMoveAnchor
	}
	return rows[0], nil
}

func neighborPosition(tx *gorm.DB, listID, taskID uint, from anchorRow, after bool) (*int64, error) {
	query := "SELECT position FROM tasks WHERE list_id = ? AND id <> ? AND deleted_at IS NULL " +
		"AND (position, id) > (?, ?) ORDER BY position, id LIMIT 1"
	if !after {
		query = "SELECT position FROM tasks WHERE list_id = ? AND id <> ? AND deleted_at IS NULL " +
			"AND (position, id) < (?, ?) ORDER BY position DESC, id DESC LIMIT 1"
	}
	var rows []int64
	if err := tx.Raw(query, listID, taskID, from.Position, from.ID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

func neighbors(tx *gorm.DB, listID, taskID uint, target domain.TaskMove) (prev, next *int64, err error) {
	switch {
	case target.AfterID != nil:
		anchor, err := loadAnchor(tx, listID, taskID, *target.AfterID)
		if err != nil {
			return nil, nil, err
		}
		next, err = neighborPosition(tx, listID, taskID, anchor, true)
		return &anchor.Position, next, err
	case target.BeforeID != nil:
		anchor, err := loadAnchor(tx, listID, taskID, *target.BeforeID)
		if err != nil {
			return nil, nil, err
		}
		prev, err = neighborPosition(tx, listID, taskID, anchor, false)
		return prev, &anchor.Position, err
	default:
		last, err := lastPosition(tx, listID, taskID)
		return &last, nil, err
	}
}

func positionBetween(prev, next *int64) (int64, bool) {
	switch {
	case prev == nil && next == nil:
		return positionStep, true
	case next == nil:
		return *prev + positionStep, true
	case prev == nil:
		return *next - positionStep, true
	case *next-*prev >= 2:
		return *prev + (*next-*prev)/2, true
	}
	return 0, false
}

func rebalance(tx *gorm.DB, listID, taskID uint) error {
	return tx.Exec(
		"UPDATE tasks t SET position = r.rn * ? FROM ("+
			"SELECT id, row_number() OVER (ORDER BY position, id) AS rn FROM tasks "+
			"WHERE list_id = ? AND id <> ? AND deleted_at IS NULL) r WHERE t.id = r.id",
		positionStep, listID, taskID,
	).Error
}

func resolvePosition(tx *gorm.DB, listID, taskID uint, target domain.TaskMove) (int64, error) {
	for attempt := 0; attempt < 2; attempt++ {
		prev, next, err := neighbors(tx, listID, taskID, target)
		if err != nil {
			return 0, err
		}
		if position, ok := positionBetween(prev, next); ok {
			return position, nil
		}
		if err := rebalance(tx, listID, taskID); err != nil {
			return 0, err
		}
	}
	return 0, errors.New("no free position after rebalancing the list")
}

func (r *taskRepository) Move(ctx context.Context, id uint, target domain.TaskMove) (*domain.Task, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var seen persistence.Task
		if err := tx.Select("id", "list_id").Where("id = ?", id).First(&seen).Error; err != nil {
			return err
		}
		if err := lockLists(tx, seen.ListID, target.ListID); err != nil {
			return err
		}
		var task persistence.Task
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", id).First(&task).Error; err != nil {
			return err
		}
		if task.ListID != seen.ListID {
			return domain.ErrConcurrentMove
		}

		var targetSpaceID uint
		if err := tx.Raw(
			"SELECT l.space_id FROM lists l "+
				"JOIN spaces s ON s.id = l.space_id AND s.deleted_at IS NULL "+
				"JOIN spaces cur ON cur.id = ? "+
				"WHERE l.id = ? AND l.deleted_at IS NULL AND s.workspace_id = cur.workspace_id",
			task.SpaceID, target.ListID,
		).Scan(&targetSpaceID).Error; err != nil {
			return err
		}
		if targetSpaceID == 0 {
			return domain.ErrInvalidListReference
		}

		var statusCount int64
		if err := tx.Model(&persistence.TaskStatus{}).
			Where("id = ? AND list_id = ?", target.StatusID, target.ListID).
			Count(&statusCount).Error; err != nil {
			return err
		}
		if statusCount != 1 {
			return domain.ErrInvalidStatusReference
		}

		listChanged := task.ListID != target.ListID
		if listChanged {
			var children int64
			if err := tx.Model(&persistence.Task{}).Where("parent_id = ?", id).Count(&children).Error; err != nil {
				return err
			}
			if children > 0 {
				return domain.ErrTaskHasSubtasks
			}
		}

		position, err := resolvePosition(tx, target.ListID, id, target)
		if err != nil {
			return err
		}

		updates := map[string]any{
			"list_id":   target.ListID,
			"space_id":  targetSpaceID,
			"status_id": target.StatusID,
			"position":  position,
		}
		if listChanged {
			updates["parent_id"] = nil
		}
		if err := tx.Model(&persistence.Task{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
		if targetSpaceID != task.SpaceID {
			return tx.Exec("DELETE FROM task_tags WHERE task_id = ?", id).Error
		}
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return r.GetByID(ctx, id)
}
