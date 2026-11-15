package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

// TaskRecurrenceStore holds the queries behind repeating tasks: what a status means, where the next task starts, what is
// carried over to it. Everything runs in the transaction of the request that completed the task.
type TaskRecurrenceStore struct {
	db *gorm.DB
}

func NewTaskRecurrenceStore(db *gorm.DB) *TaskRecurrenceStore {
	return &TaskRecurrenceStore{db: db}
}

// IsDoneStatus reports whether the status counts as finished.
func (s *TaskRecurrenceStore) IsDoneStatus(ctx context.Context, statusID uint) (bool, error) {
	var done bool
	err := database.DBFromContext(ctx, s.db).Raw(
		"SELECT COALESCE((SELECT type = 'done' FROM task_statuses WHERE id = ? AND deleted_at IS NULL), false)", statusID).Scan(&done).Error
	return done, mapErr(err)
}

// OpenStatus is the status a new occurrence starts in: the first status of the list that is not finished.
func (s *TaskRecurrenceStore) OpenStatus(ctx context.Context, listID uint) (uint, error) {
	var ids []uint
	err := database.DBFromContext(ctx, s.db).Raw(`
		SELECT id FROM task_statuses WHERE list_id = ? AND deleted_at IS NULL
		ORDER BY (type = 'done'), position, id LIMIT 1`, listID).Scan(&ids).Error
	if err != nil {
		return 0, mapErr(err)
	}
	if len(ids) == 0 {
		return 0, domain.ErrNotFound
	}
	return ids[0], nil
}

// CarryOver gives the next occurrence the custom field values and the checklists of the completed task; every
// checklist item starts unresolved. Comments, attachments, time entries, subtasks and relations stay with the old task.
func (s *TaskRecurrenceStore) CarryOver(ctx context.Context, fromTaskID, toTaskID uint) error {
	tx := database.DBFromContext(ctx, s.db)
	if err := tx.Exec(`
		INSERT INTO task_custom_field_values (task_id, custom_field_id, workspace_id, value)
		SELECT ?, custom_field_id, workspace_id, value FROM task_custom_field_values WHERE task_id = ?`,
		toTaskID, fromTaskID).Error; err != nil {
		return mapErr(err)
	}
	var checklists []struct {
		ID       uint
		Name     string
		Position int64
	}
	if err := tx.Raw("SELECT id, name, position FROM task_checklists WHERE task_id = ? ORDER BY position, id", fromTaskID).
		Scan(&checklists).Error; err != nil {
		return mapErr(err)
	}
	for _, source := range checklists {
		var created []uint
		if err := tx.Raw("INSERT INTO task_checklists (task_id, name, position) VALUES (?, ?, ?) RETURNING id",
			toTaskID, source.Name, source.Position).Scan(&created).Error; err != nil {
			return mapErr(err)
		}
		if err := tx.Exec(`
			INSERT INTO task_checklist_items (checklist_id, text, assignee_id, position)
			SELECT ?, text, assignee_id, position FROM task_checklist_items WHERE checklist_id = ?`,
			created[0], source.ID).Error; err != nil {
			return mapErr(err)
		}
	}
	return nil
}

// ClearRecurrence removes the rule of a task: the series moved on to another task or came to an end.
func (s *TaskRecurrenceStore) ClearRecurrence(ctx context.Context, taskID uint) error {
	return mapErr(database.DBFromContext(ctx, s.db).Exec("UPDATE tasks SET recurrence = NULL WHERE id = ?", taskID).Error)
}

// DueScheduled lists the tasks whose scheduled rule is due: the next occurrence has to be created. Oldest first.
func (s *TaskRecurrenceStore) DueScheduled(ctx context.Context, now time.Time, limit int) ([]uint, error) {
	var ids []uint
	err := database.DBFromContext(ctx, s.db).Raw(`
		SELECT id FROM tasks
		WHERE deleted_at IS NULL AND recurrence ->> 'trigger' = 'schedule' AND due_date <= ?
		ORDER BY due_date, id LIMIT ?`, now, limit).Scan(&ids).Error
	return ids, mapErr(err)
}

// ClaimScheduled locks the task while its next occurrence is created and reports whether the task is still due. A task
// another scheduler instance holds, or one that changed since DueScheduled, is not claimed.
func (s *TaskRecurrenceStore) ClaimScheduled(ctx context.Context, taskID uint, now time.Time) (bool, error) {
	var ids []uint
	err := database.DBFromContext(ctx, s.db).Raw(`
		SELECT id FROM tasks
		WHERE id = ? AND deleted_at IS NULL AND recurrence ->> 'trigger' = 'schedule' AND due_date <= ?
		FOR UPDATE SKIP LOCKED`, taskID, now).Scan(&ids).Error
	return len(ids) == 1, mapErr(err)
}
