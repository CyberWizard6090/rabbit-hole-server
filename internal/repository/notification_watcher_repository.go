package repository

import (
	"context"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type watcherRepository struct {
	db *gorm.DB
}

func NewWatcherRepository(db *gorm.DB) domain.WatcherRepository {
	return &watcherRepository{db: db}
}

func (r *watcherRepository) List(ctx context.Context, taskID uint) ([]domain.CommentUser, error) {
	var rows []commentUserRow
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT u.id, u.username, u.first_name, u.last_name, u.avatar_url FROM task_watchers tw "+
			"JOIN users u ON u.id = tw.user_id "+
			"JOIN tasks t ON t.id = tw.task_id "+
			"JOIN spaces s ON s.id = t.space_id "+
			"JOIN workspace_members wm ON wm.workspace_id = s.workspace_id AND wm.user_id = tw.user_id AND wm.deleted_at IS NULL "+
			"JOIN workspaces w ON w.id = s.workspace_id AND w.deleted_at IS NULL "+
			"WHERE tw.task_id = ? AND tw.muted = false AND "+liveTaskPredicate("t")+" ORDER BY tw.created_at, u.id", taskID,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.CommentUser, len(rows))
	for i, row := range rows {
		out[i] = row.toDomain()
	}
	return out, nil
}

func (r *watcherRepository) State(ctx context.Context, taskID, userID uint) (domain.WatchState, error) {
	var muted []bool
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT muted FROM task_watchers WHERE task_id = ? AND user_id = ?", taskID, userID,
	).Scan(&muted).Error
	if err != nil {
		return domain.WatchState{}, mapErr(err)
	}
	if len(muted) == 0 {
		return domain.WatchState{}, nil
	}
	return domain.WatchState{Watching: !muted[0], Muted: muted[0]}, nil
}

func (r *watcherRepository) Watch(ctx context.Context, taskID, userID uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		"INSERT INTO task_watchers (task_id, user_id, muted) VALUES (?, ?, false) "+
			"ON CONFLICT (task_id, user_id) DO UPDATE SET muted = false", taskID, userID,
	).Error)
}

func (r *watcherRepository) Unwatch(ctx context.Context, taskID, userID uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		"INSERT INTO task_watchers (task_id, user_id, muted) VALUES (?, ?, true) "+
			"ON CONFLICT (task_id, user_id) DO UPDATE SET muted = true", taskID, userID,
	).Error)
}

func (r *watcherRepository) AutoWatch(ctx context.Context, taskID uint, userIDs []uint) error {
	userIDs = uniqueIDs(userIDs)
	if len(userIDs) == 0 {
		return nil
	}
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		for _, id := range userIDs {
			if err := tx.Exec(
				"INSERT INTO task_watchers (task_id, user_id) VALUES (?, ?) ON CONFLICT (task_id, user_id) DO NOTHING",
				taskID, id,
			).Error; err != nil {
				return err
			}
		}
		return nil
	}))
}

func (r *watcherRepository) ActiveWatcherIDs(ctx context.Context, taskID uint) ([]uint, error) {
	var ids []uint
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT user_id FROM task_watchers WHERE task_id = ? AND muted = false ORDER BY user_id", taskID,
	).Scan(&ids).Error
	return ids, mapErr(err)
}
