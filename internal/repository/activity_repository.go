package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type activityRepository struct {
	db *gorm.DB
}

func NewActivityRepository(db *gorm.DB) domain.ActivityRepository {
	return &activityRepository{db: db}
}

func jsonArg(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}

func (r *activityRepository) Record(ctx context.Context, entries []domain.ActivityEntry) error {
	if len(entries) == 0 {
		return nil
	}
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		for _, e := range entries {
			var field any
			if e.Field != "" {
				field = e.Field
			}
			if err := tx.Exec(
				"INSERT INTO task_activities (task_id, user_id, action, field, old_value, new_value) "+
					"VALUES (?, ?, ?, ?, ?::jsonb, ?::jsonb)",
				e.TaskID, e.ActorID, e.Action, field, jsonArg(e.OldValue), jsonArg(e.NewValue),
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return mapErr(err)
}

type activityRow struct {
	ID        uint
	CreatedAt time.Time
	TaskID    uint
	UserID    uint
	Action    string
	Field     *string
	OldValue  []byte
	NewValue  []byte
}

func (r *activityRepository) ListByTask(ctx context.Context, taskID uint, limit, offset int) ([]domain.Activity, int64, error) {
	db := database.DBFromContext(ctx, r.db)

	var total int64
	if err := db.Raw("SELECT count(*) FROM task_activities WHERE task_id = ?", taskID).Scan(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}

	var rows []activityRow
	if err := db.Raw(
		"SELECT id, created_at, task_id, user_id, action, field, old_value, new_value "+
			"FROM task_activities WHERE task_id = ? ORDER BY id DESC LIMIT ? OFFSET ?",
		taskID, limit, offset,
	).Scan(&rows).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	if len(rows) == 0 {
		return []domain.Activity{}, total, nil
	}

	actorIDs := make([]uint, len(rows))
	for i, row := range rows {
		actorIDs[i] = row.UserID
	}
	var actors []commentUserRow
	if err := db.Table("users").Select("id, username, first_name, last_name, avatar_url").
		Where("id IN ?", uniqueIDs(actorIDs)).Scan(&actors).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	actorByID := make(map[uint]commentUserRow, len(actors))
	for _, a := range actors {
		actorByID[a.ID] = a
	}

	out := make([]domain.Activity, len(rows))
	for i, row := range rows {
		activity := domain.Activity{
			ID: row.ID, TaskID: row.TaskID, Actor: actorByID[row.UserID].toDomain(),
			Action: row.Action, CreatedAt: row.CreatedAt,
			OldValue: row.OldValue, NewValue: row.NewValue,
		}
		if row.Field != nil {
			activity.Field = *row.Field
		}
		out[i] = activity
	}
	return out, total, nil
}
