package repository

import (
	"context"
	"strings"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

const relationLockClass = 4711

type taskRelationRepository struct {
	db *gorm.DB
}

func NewTaskRelationRepository(db *gorm.DB) domain.TaskRelationRepository {
	return &taskRelationRepository{db: db}
}

func orient(taskID, otherID uint, kind string) (from, to uint, stored string, err error) {
	switch kind {
	case domain.RelationBlocks:
		return taskID, otherID, "blocks", nil
	case domain.RelationBlockedBy:
		return otherID, taskID, "blocks", nil
	case domain.RelationRelated:
		if taskID < otherID {
			return taskID, otherID, "related", nil
		}
		return otherID, taskID, "related", nil
	default:
		return 0, 0, "", domain.ErrRelationBadKind
	}
}

func (r *taskRelationRepository) Add(ctx context.Context, taskID, otherID uint, kind string, userID uint) error {
	if taskID == otherID {
		return domain.ErrRelationSelf
	}
	from, to, stored, err := orient(taskID, otherID, kind)
	if err != nil {
		return err
	}
	return mapErr(database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		var spaces []struct{ WorkspaceID uint }
		if err := tx.Raw(`SELECT s.workspace_id FROM tasks t JOIN spaces s ON s.id = t.space_id AND s.deleted_at IS NULL
			WHERE t.id IN (?, ?) AND t.deleted_at IS NULL`, taskID, otherID).Scan(&spaces).Error; err != nil {
			return err
		}
		if len(spaces) != 2 || spaces[0].WorkspaceID != spaces[1].WorkspaceID {
			return domain.ErrRelationOtherTask
		}
		lockKey := int32(spaces[0].WorkspaceID) // #nosec G115 -- only a lock key, collisions are harmless
		if err := tx.Exec("SELECT pg_advisory_xact_lock(?, ?)", relationLockClass, lockKey).Error; err != nil {
			return err
		}
		for _, id := range []uint{taskID, otherID} {
			var n int64
			if err := tx.Raw("SELECT count(*) FROM task_relations WHERE from_task_id = ? OR to_task_id = ?", id, id).Scan(&n).Error; err != nil {
				return err
			}
			if n >= domain.MaxRelationsPerTask {
				return domain.ErrRelationLimit
			}
		}
		if stored == "blocks" {
			var cyclic bool
			if err := tx.Raw(`WITH RECURSIVE reach(id) AS (
					SELECT ?::bigint
					UNION
					SELECT r.to_task_id FROM task_relations r JOIN reach ON r.from_task_id = reach.id WHERE r.kind = 'blocks')
				SELECT EXISTS (SELECT 1 FROM reach WHERE id = ?)`, to, from).Scan(&cyclic).Error; err != nil {
				return err
			}
			if cyclic {
				return domain.ErrRelationCycle
			}
		}
		if err := tx.Exec(`INSERT INTO task_relations (from_task_id, to_task_id, kind, created_by) VALUES (?, ?, ?, ?)`,
			from, to, stored, userID).Error; err != nil {
			if strings.Contains(err.Error(), "uq_task_relations") {
				return domain.ErrRelationExists
			}
			return err
		}
		return nil
	}))
}

func (r *taskRelationRepository) Remove(ctx context.Context, taskID, otherID uint, kind string) error {
	from, to, stored, err := orient(taskID, otherID, kind)
	if err != nil {
		return err
	}
	res := database.DBFromContext(ctx, r.db).Exec(
		"DELETE FROM task_relations WHERE from_task_id = ? AND to_task_id = ? AND kind = ?", from, to, stored)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrRelationMissing
	}
	return nil
}

func (r *taskRelationRepository) List(ctx context.Context, taskID uint) (*domain.TaskRelations, error) {
	var rows []struct {
		Kind     string
		Outgoing bool
		ID       uint
		Title    string
		ListID   uint
		StatusID uint
		Done     bool
	}
	err := database.DBFromContext(ctx, r.db).Raw(`
		SELECT r.kind, (r.from_task_id = ?) AS outgoing, t.id, t.title, t.list_id, t.status_id, (ts.type = 'done') AS done
		FROM task_relations r
		JOIN tasks t ON t.id = CASE WHEN r.from_task_id = ? THEN r.to_task_id ELSE r.from_task_id END AND t.deleted_at IS NULL
		JOIN task_statuses ts ON ts.id = t.status_id
		WHERE r.from_task_id = ? OR r.to_task_id = ?
		ORDER BY r.id`, taskID, taskID, taskID, taskID).Scan(&rows).Error
	if err != nil {
		return nil, mapErr(err)
	}
	out := &domain.TaskRelations{Blocks: []domain.RelatedTask{}, BlockedBy: []domain.RelatedTask{}, Related: []domain.RelatedTask{}}
	for _, row := range rows {
		item := domain.RelatedTask{ID: row.ID, Title: row.Title, ListID: row.ListID, StatusID: row.StatusID, Done: row.Done}
		switch {
		case row.Kind == "related":
			out.Related = append(out.Related, item)
		case row.Outgoing:
			out.Blocks = append(out.Blocks, item)
		default:
			out.BlockedBy = append(out.BlockedBy, item)
			if !row.Done {
				out.Blocked = true
			}
		}
	}
	return out, nil
}
