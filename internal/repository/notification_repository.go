package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
)

const readableTaskFilter = "EXISTS (SELECT 1 FROM tasks t " +
	"JOIN spaces s ON s.id = t.space_id AND s.deleted_at IS NULL " +
	"JOIN workspaces w ON w.id = s.workspace_id AND w.deleted_at IS NULL " +
	"JOIN workspace_members wm ON wm.workspace_id = w.id AND wm.user_id = n.user_id AND wm.deleted_at IS NULL " +
	"JOIN roles r ON r.id = wm.role_id AND r.deleted_at IS NULL " +
	"JOIN role_permissions rp ON rp.role_id = r.id AND rp.deleted_at IS NULL " +
	"JOIN permissions p ON p.id = rp.permission_id AND p.deleted_at IS NULL AND p.code = '" + domain.PermTaskRead + "' " +
	"WHERE t.id = n.task_id AND t.deleted_at IS NULL)"

type notificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) domain.NotificationRepository {
	return &notificationRepository{db: db}
}

func (r *notificationRepository) Create(ctx context.Context, d domain.NotificationDraft, keep int) (domain.NotificationRef, error) {
	payload := d.Payload
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}
	var ref domain.NotificationRef
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var rows []struct {
			ID        uint
			CreatedAt time.Time
		}
		if err := tx.Raw(
			"INSERT INTO notifications (user_id, type, task_id, actor_id, workspace_id, payload) VALUES (?, ?, ?, ?, ?, ?::jsonb) "+
				"RETURNING id, created_at",
			d.UserID, d.Type, d.TaskID, d.ActorID, d.WorkspaceID, string(payload),
		).Scan(&rows).Error; err != nil {
			return err
		}
		if len(rows) != 1 {
			return domain.ErrNotFound
		}
		ref = domain.NotificationRef{ID: rows[0].ID, CreatedAt: rows[0].CreatedAt}
		return tx.Exec(
			"DELETE FROM notifications WHERE user_id = ? AND id NOT IN "+
				"(SELECT id FROM notifications WHERE user_id = ? ORDER BY id DESC LIMIT ?)",
			d.UserID, d.UserID, keep,
		).Error
	})
	return ref, mapErr(err)
}

type notificationRow struct {
	ID          uint
	Type        string
	TaskID      uint
	WorkspaceID uint
	ActorID     uint
	Payload     []byte
	CreatedAt   time.Time
	ReadAt      *time.Time
}

func (r *notificationRepository) List(
	ctx context.Context, userID uint, filter domain.NotificationFilter, limit, offset int,
) ([]domain.Notification, int64, error) {
	where := "n.user_id = ? AND " + readableTaskFilter
	args := []any{userID}
	if filter.UnreadOnly {
		where += " AND n.read_at IS NULL"
	}
	if filter.Type != "" {
		where += " AND n.type = ?"
		args = append(args, filter.Type)
	}
	db := r.db.WithContext(ctx)

	var total int64
	if err := db.Raw("SELECT count(*) FROM notifications n WHERE "+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	var rows []notificationRow
	if err := db.Raw(
		"SELECT n.id, n.type, n.task_id, n.workspace_id, n.actor_id, n.payload, n.created_at, n.read_at "+
			"FROM notifications n WHERE "+where+" ORDER BY n.id DESC LIMIT ? OFFSET ?",
		append(args, limit, offset)...,
	).Scan(&rows).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	if len(rows) == 0 {
		return []domain.Notification{}, total, nil
	}
	actorIDs := make([]uint, len(rows))
	for i, row := range rows {
		actorIDs[i] = row.ActorID
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
	out := make([]domain.Notification, len(rows))
	for i, row := range rows {
		out[i] = domain.Notification{
			ID: row.ID, Type: row.Type, TaskID: row.TaskID, WorkspaceID: row.WorkspaceID,
			Actor: actorByID[row.ActorID].toDomain(), Payload: row.Payload,
			CreatedAt: row.CreatedAt, ReadAt: row.ReadAt,
		}
	}
	return out, total, nil
}

func (r *notificationRepository) UnreadCounts(ctx context.Context, userID uint) (map[string]int64, error) {
	var rows []struct {
		Type  string
		Count int64
	}
	err := r.db.WithContext(ctx).Raw(
		"SELECT n.type, count(*) AS count FROM notifications n "+
			"WHERE n.user_id = ? AND n.read_at IS NULL AND "+readableTaskFilter+" GROUP BY n.type", userID,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	out := make(map[string]int64, len(rows))
	for _, row := range rows {
		out[row.Type] = row.Count
	}
	return out, nil
}

func (r *notificationRepository) MarkRead(ctx context.Context, userID uint, ids []uint, all bool, typeKey string) (int64, error) {
	query := "UPDATE notifications n SET read_at = now() WHERE n.user_id = ? AND n.read_at IS NULL AND " + readableTaskFilter
	args := []any{userID}
	if !all {
		query += " AND n.id IN ?"
		args = append(args, ids)
	}
	if typeKey != "" {
		query += " AND n.type = ?"
		args = append(args, typeKey)
	}
	res := r.db.WithContext(ctx).Exec(query, args...)
	return res.RowsAffected, mapErr(res.Error)
}

func (r *notificationRepository) Delete(ctx context.Context, userID, id uint) error {
	res := r.db.WithContext(ctx).Exec("DELETE FROM notifications WHERE id = ? AND user_id = ?", id, userID)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

type notificationSourceRepository struct {
	db *gorm.DB
}

func NewNotificationSourceRepository(db *gorm.DB) domain.NotificationSourceRepository {
	return &notificationSourceRepository{db: db}
}

func (r *notificationSourceRepository) TaskInfo(ctx context.Context, taskID uint) (*domain.TaskNotificationInfo, error) {
	var rows []domain.TaskNotificationInfo
	err := r.db.WithContext(ctx).Raw(
		"SELECT t.id, t.title, s.workspace_id FROM tasks t JOIN spaces s ON s.id = t.space_id "+
			"WHERE t.id = ? AND t.deleted_at IS NULL", taskID,
	).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	return &rows[0], nil
}

func (r *notificationSourceRepository) StatusName(ctx context.Context, statusID uint) (string, error) {
	var names []string
	err := r.db.WithContext(ctx).Raw("SELECT name FROM task_statuses WHERE id = ?", statusID).Scan(&names).Error
	if err != nil {
		return "", mapErr(err)
	}
	if len(names) == 0 {
		return "", domain.ErrNotFound
	}
	return names[0], nil
}

func (r *notificationSourceRepository) User(ctx context.Context, userID uint) (*domain.CommentUser, error) {
	var rows []commentUserRow
	err := r.db.WithContext(ctx).Table("users").Select("id, username, first_name, last_name, avatar_url").
		Where("id = ?", userID).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	u := rows[0].toDomain()
	return &u, nil
}

func (r *notificationSourceRepository) AccountMail(ctx context.Context, userID uint) (*domain.AccountMailTarget, error) {
	var rows []struct {
		Email           string
		EmailVerifiedAt *time.Time
	}
	err := r.db.WithContext(ctx).Raw("SELECT email, email_verified_at FROM users WHERE id = ?", userID).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	return &domain.AccountMailTarget{Email: rows[0].Email, Verified: rows[0].EmailVerifiedAt != nil}, nil
}
