package repository

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
)

type taskRepository struct {
	db *gorm.DB
}

func NewTaskRepository(db *gorm.DB) domain.TaskRepository {
	return &taskRepository{db: db}
}

func (r *taskRepository) Create(task *domain.Task, assigneeIDs []uint, tagIDs []uint) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := validateTaskReferences(tx, task, assigneeIDs, tagIDs); err != nil {
			return err
		}
		if len(assigneeIDs) > 0 {
			var users []domain.User
			if err := tx.Find(&users, assigneeIDs).Error; err != nil {
				return err
			}
			task.Assignees = users
		}

		if len(tagIDs) > 0 {
			var tags []domain.Tag
			if err := tx.Find(&tags, tagIDs).Error; err != nil {
				return err
			}
			task.Tags = tags
		}

		return tx.Create(task).Error
	})
}

func (r *taskRepository) List(
	listID uint,
	params pagination.Params,
) ([]domain.Task, bool, bool, error) {
	fetchLimit, err := pagination.FetchLimit(params.Limit)
	if err != nil {
		return nil, false, false, err
	}

	var tasks []domain.Task

	query := r.db.
		Where("list_id = ?", listID).
		Preload("Assignees").
		Preload("Tags")

	if params.Cursor != nil {
		cursor := params.Cursor
		position, err := decodeTaskCursorPosition(cursor)
		if err != nil {
			return nil, false, false, err
		}

		switch cursor.Direction {
		case pagination.DirectionNext:
			query = query.Where(
				"(created_at, id) < (?, ?)",
				position.CreatedAt,
				position.ID,
			)
		case pagination.DirectionPrev:
			query = query.Where(
				"(created_at, id) > (?, ?)",
				position.CreatedAt,
				position.ID,
			)
		default:
			return nil, false, false, fmt.Errorf(
				"unsupported cursor direction: %q",
				cursor.Direction,
			)
		}
	}

	order := "created_at DESC, id DESC"

	if params.Cursor != nil && params.Cursor.Direction == pagination.DirectionPrev {
		order = "created_at ASC, id ASC"
	}

	err = query.
		Order(order).
		Limit(fetchLimit).
		Find(&tasks).Error
	if err != nil {
		return nil, false, false, err
	}

	hasMore := len(tasks) > params.Limit
	if hasMore {
		tasks = tasks[:params.Limit]
	}

	hasPrev := hasPreviousPage(params.Cursor, hasMore)

	if params.Cursor != nil && params.Cursor.Direction == pagination.DirectionPrev {
		for i, j := 0, len(tasks)-1; i < j; i, j = i+1, j-1 {
			tasks[i], tasks[j] = tasks[j], tasks[i]
		}
	}

	hasNext := hasMore
	if params.Cursor != nil && params.Cursor.Direction == pagination.DirectionPrev {
		// A previous-page request can become stale after deletions. An empty
		// result must not advertise a next page without a usable cursor.
		hasNext = len(tasks) > 0
	}

	return tasks, hasNext, hasPrev, nil
}

func decodeTaskCursorPosition(cursor *pagination.Cursor) (pagination.TaskCursorPosition, error) {
	var position pagination.TaskCursorPosition
	if cursor == nil {
		return position, fmt.Errorf("%w: cursor is required", pagination.ErrInvalidCursor)
	}
	if err := json.Unmarshal(cursor.Position, &position); err != nil || position.ID == 0 || position.ID > uint64(1<<63-1) {
		return pagination.TaskCursorPosition{}, fmt.Errorf("%w: invalid task cursor position", pagination.ErrInvalidCursor)
	}
	return position, nil
}

func hasPreviousPage(cursor *pagination.Cursor, hasMore bool) bool {
	if cursor == nil {
		return false
	}

	switch cursor.Direction {
	case pagination.DirectionNext:
		return true
	case pagination.DirectionPrev:
		return hasMore
	default:
		return false
	}
}

func (r *taskRepository) GetByID(id uint) (*domain.Task, error) {
	var task domain.Task
	err := r.db.Where("id = ?", id).
		Preload("Assignees").
		Preload("Tags").
		First(&task).Error
	return &task, err
}

func (r *taskRepository) Update(task *domain.Task) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := validateTaskReferences(tx, task, nil, nil); err != nil {
			return err
		}
		return tx.Save(task).Error
	})
}

func (r *taskRepository) Delete(id uint) error {
	res := r.db.Where("id = ?", id).Delete(&domain.Task{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (r *taskRepository) AddTags(taskID uint, tagIDs []uint) error {
	if len(tagIDs) == 0 {
		return nil
	}

	var task domain.Task
	if err := r.db.First(&task, taskID).Error; err != nil {
		return err
	}
	if err := validateTagReferences(r.db, task.SpaceID, tagIDs); err != nil {
		return err
	}

	var tags []domain.Tag
	if err := r.db.Find(&tags, tagIDs).Error; err != nil {
		return err
	}

	return r.db.Model(&task).Association("Tags").Append(tags)
}

func validateTaskReferences(tx *gorm.DB, task *domain.Task, assigneeIDs, tagIDs []uint) error {
	var statusCount int64
	if err := tx.Model(&domain.TaskStatus{}).
		Where("id = ? AND list_id = ?", task.StatusID, task.ListID).
		Count(&statusCount).Error; err != nil {
		return err
	}
	if statusCount != 1 {
		return domain.ErrInvalidStatusReference
	}

	if task.ParentID != nil {
		var parentCount int64
		if err := tx.Model(&domain.Task{}).
			Where("id = ? AND list_id = ?", *task.ParentID, task.ListID).
			Count(&parentCount).Error; err != nil {
			return err
		}
		if parentCount != 1 {
			return domain.ErrInvalidParentReference
		}
	}

	if err := validateTagReferences(tx, task.SpaceID, tagIDs); err != nil {
		return err
	}
	assigneeIDs = uniqueIDs(assigneeIDs)
	if len(assigneeIDs) == 0 {
		return nil
	}

	var assigneeCount int64
	if err := tx.Table("users").
		Joins("JOIN workspace_members ON workspace_members.user_id = users.id").
		Joins("JOIN spaces ON spaces.workspace_id = workspace_members.workspace_id").
		Where("spaces.id = ? AND users.id IN ?", task.SpaceID, assigneeIDs).
		Distinct("users.id").Count(&assigneeCount).Error; err != nil {
		return err
	}
	if assigneeCount != int64(len(assigneeIDs)) {
		return domain.ErrInvalidAssigneeReference
	}
	return nil
}

func validateTagReferences(tx *gorm.DB, spaceID uint, tagIDs []uint) error {
	tagIDs = uniqueIDs(tagIDs)
	if len(tagIDs) == 0 {
		return nil
	}

	var tagCount int64
	if err := tx.Model(&domain.Tag{}).
		Where("space_id = ? AND id IN ?", spaceID, tagIDs).
		Count(&tagCount).Error; err != nil {
		return err
	}
	if tagCount != int64(len(tagIDs)) {
		return domain.ErrInvalidTagReference
	}
	return nil
}

func uniqueIDs(ids []uint) []uint {
	seen := make(map[uint]struct{}, len(ids))
	result := make([]uint, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
