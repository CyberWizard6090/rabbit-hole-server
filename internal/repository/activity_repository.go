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
			actor := e.Actor.Normalized()
			var userID, actorID, label any
			if actor.ID != 0 {
				actorID = actor.ID
			}
			if actor.Type == domain.ActorUser {
				userID = actor.ID
			}
			if actor.Label != "" {
				label = actor.Label
			}
			if err := tx.Exec(
				"INSERT INTO task_activities (task_id, user_id, actor_type, actor_id, actor_label, action, field, old_value, new_value) "+
					"VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb)",
				e.TaskID, userID, string(actor.Type), actorID, label, e.Action, field, jsonArg(e.OldValue), jsonArg(e.NewValue),
			).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return mapErr(err)
}

type activityRow struct {
	ID         uint
	CreatedAt  time.Time
	TaskID     uint
	UserID     *uint
	ActorType  string
	ActorID    *uint
	ActorLabel *string
	Action     string
	Field      *string
	OldValue   []byte
	NewValue   []byte
}

func (row activityRow) actor() domain.Actor {
	actor := domain.Actor{Type: domain.ActorType(row.ActorType)}
	if actor.Type == domain.ActorUser {
		if row.UserID != nil {
			actor.ID = *row.UserID
		}
		return actor
	}
	if row.ActorID != nil {
		actor.ID = *row.ActorID
	}
	if row.ActorLabel != nil {
		actor.Label = *row.ActorLabel
	}
	return actor
}

func (r *activityRepository) ListByTask(ctx context.Context, taskID uint, limit, offset int) ([]domain.Activity, int64, error) {
	db := database.DBFromContext(ctx, r.db)

	var total int64
	if err := db.Raw("SELECT count(*) FROM task_activities WHERE task_id = ?", taskID).Scan(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}

	var rows []activityRow
	if err := db.Raw(
		"SELECT id, created_at, task_id, user_id, actor_type, actor_id, actor_label, action, field, old_value, new_value "+
			"FROM task_activities WHERE task_id = ? ORDER BY id DESC LIMIT ? OFFSET ?",
		taskID, limit, offset,
	).Scan(&rows).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	if len(rows) == 0 {
		return []domain.Activity{}, total, nil
	}

	var userIDs []uint
	for _, row := range rows {
		if row.UserID != nil {
			userIDs = append(userIDs, *row.UserID)
		}
	}
	users, err := loadPublicUsers(db, userIDs)
	if err != nil {
		return nil, 0, mapErr(err)
	}

	out := make([]domain.Activity, len(rows))
	for i, row := range rows {
		actor := row.actor()
		var user *domain.CommentUser
		if u, ok := users[actor.UserID()]; ok {
			user = &u
		}
		activity := domain.Activity{
			ID: row.ID, TaskID: row.TaskID, Actor: domain.NewActorView(actor, user),
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
