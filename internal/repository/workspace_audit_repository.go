package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type workspaceAuditRepository struct {
	db *gorm.DB
}

func NewWorkspaceAuditRepository(db *gorm.DB) domain.WorkspaceAuditRepository {
	return &workspaceAuditRepository{db: db}
}

func (r *workspaceAuditRepository) Record(ctx context.Context, draft domain.WorkspaceAuditDraft) error {
	var actor, target any
	if draft.ActorID != 0 {
		actor = draft.ActorID
	}
	if draft.TargetUserID != 0 {
		target = draft.TargetUserID
	}
	changes := string(draft.Changes)
	if changes == "" {
		changes = "{}"
	}
	return mapErr(database.DBFromContext(ctx, r.db).Exec(
		`INSERT INTO workspace_audit_log (workspace_id, actor_id, target_user_id, action, changes) VALUES (?, ?, ?, ?, ?::jsonb)`,
		draft.WorkspaceID, actor, target, draft.Action, changes,
	).Error)
}

type workspaceAuditRow struct {
	ID           uint
	ActorID      *uint
	TargetUserID *uint
	Action       string
	Changes      []byte
	CreatedAt    time.Time
}

func (r *workspaceAuditRepository) List(
	ctx context.Context, workspaceID uint, action string, limit, offset int,
) ([]domain.WorkspaceAuditEntry, int64, error) {
	db := database.DBFromContext(ctx, r.db)
	where := "workspace_id = ?"
	args := []any{workspaceID}
	if action != "" {
		where += " AND action = ?"
		args = append(args, action)
	}
	var total int64
	if err := db.Raw("SELECT count(*) FROM workspace_audit_log WHERE "+where, args...).Scan(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	var rows []workspaceAuditRow
	if err := db.Raw(
		"SELECT id, actor_id, target_user_id, action, changes, created_at FROM workspace_audit_log WHERE "+where+
			" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, limit, offset)...,
	).Scan(&rows).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	ids := make([]uint, 0, len(rows)*2)
	for _, row := range rows {
		if row.ActorID != nil {
			ids = append(ids, *row.ActorID)
		}
		if row.TargetUserID != nil {
			ids = append(ids, *row.TargetUserID)
		}
	}
	users, err := loadPublicUsers(db, ids)
	if err != nil {
		return nil, 0, mapErr(err)
	}
	out := make([]domain.WorkspaceAuditEntry, 0, len(rows))
	for _, row := range rows {
		entry := domain.WorkspaceAuditEntry{ID: row.ID, Action: row.Action, Changes: json.RawMessage(row.Changes), CreatedAt: row.CreatedAt}
		if row.ActorID != nil {
			if u, ok := users[*row.ActorID]; ok {
				entry.Actor = &u
			}
		}
		if row.TargetUserID != nil {
			if u, ok := users[*row.TargetUserID]; ok {
				entry.Target = &u
			}
		}
		out = append(out, entry)
	}
	return out, total, nil
}
