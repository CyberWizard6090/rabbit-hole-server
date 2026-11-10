package repository

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type checklistRepository struct {
	db *gorm.DB
}

func NewChecklistRepository(db *gorm.DB) domain.ChecklistRepository {
	return &checklistRepository{db: db}
}

type checklistRow struct {
	ID        uint
	CreatedAt time.Time
	UpdatedAt time.Time
	TaskID    uint
	Name      string
	Position  int64
}

type checklistItemRow struct {
	ID          uint
	CreatedAt   time.Time
	UpdatedAt   time.Time
	ChecklistID uint
	Text        string
	Resolved    bool
	ResolvedAt  *time.Time
	ResolvedBy  *uint
	AssigneeID  *uint
	Position    int64
}

const (
	checklistColumns = "id, created_at, updated_at, task_id, name, position"
	itemColumns      = "id, created_at, updated_at, checklist_id, text, resolved, resolved_at, resolved_by, assignee_id, position"
)

func lockTask(tx *gorm.DB, taskID uint) error {
	var ids []uint
	if err := tx.Raw("SELECT id FROM tasks WHERE id = ? FOR UPDATE", taskID).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func lockChecklist(tx *gorm.DB, taskID, checklistID uint) error {
	var ids []uint
	if err := tx.Raw(
		"SELECT id FROM task_checklists WHERE id = ? AND task_id = ? FOR UPDATE", checklistID, taskID,
	).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func nextChecklistPosition(tx *gorm.DB, table, column string, id uint) (int64, error) {
	var last int64
	err := tx.Raw("SELECT COALESCE(MAX(position), 0) FROM "+table+" WHERE "+column+" = ?", id).Scan(&last).Error
	return last + positionStep, err
}

func usersByID(tx *gorm.DB, ids []uint) (map[uint]commentUserRow, error) {
	out := map[uint]commentUserRow{}
	ids = uniqueIDs(ids)
	if len(ids) == 0 {
		return out, nil
	}
	var rows []commentUserRow
	if err := tx.Table("users").Select("id, username, first_name, last_name, avatar_url").
		Where("id IN ?", ids).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row
	}
	return out, nil
}

func hydrateItems(tx *gorm.DB, rows []checklistItemRow) ([]domain.ChecklistItem, error) {
	var userIDs []uint
	for _, row := range rows {
		if row.ResolvedBy != nil {
			userIDs = append(userIDs, *row.ResolvedBy)
		}
		if row.AssigneeID != nil {
			userIDs = append(userIDs, *row.AssigneeID)
		}
	}
	users, err := usersByID(tx, userIDs)
	if err != nil {
		return nil, err
	}
	toUser := func(id *uint) *domain.CommentUser {
		if id == nil {
			return nil
		}
		user := users[*id].toDomain()
		return &user
	}
	out := make([]domain.ChecklistItem, len(rows))
	for i, row := range rows {
		out[i] = domain.ChecklistItem{
			ID: row.ID, ChecklistID: row.ChecklistID, Text: row.Text, Resolved: row.Resolved,
			ResolvedAt: row.ResolvedAt, ResolvedBy: toUser(row.ResolvedBy), Assignee: toUser(row.AssigneeID),
			Position: row.Position, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}
	return out, nil
}

func loadChecklists(tx *gorm.DB, rows []checklistRow) ([]domain.Checklist, error) {
	if len(rows) == 0 {
		return []domain.Checklist{}, nil
	}
	ids := make([]uint, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	var itemRows []checklistItemRow
	if err := tx.Raw(
		"SELECT "+itemColumns+" FROM task_checklist_items WHERE checklist_id IN ? ORDER BY position, id", ids,
	).Scan(&itemRows).Error; err != nil {
		return nil, err
	}
	items, err := hydrateItems(tx, itemRows)
	if err != nil {
		return nil, err
	}
	byChecklist := map[uint][]domain.ChecklistItem{}
	for _, item := range items {
		byChecklist[item.ChecklistID] = append(byChecklist[item.ChecklistID], item)
	}
	out := make([]domain.Checklist, len(rows))
	for i, row := range rows {
		list := byChecklist[row.ID]
		if list == nil {
			list = []domain.ChecklistItem{}
		}
		out[i] = domain.Checklist{
			ID: row.ID, TaskID: row.TaskID, Name: row.Name, Position: row.Position, Items: list,
			CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
	}
	return out, nil
}

func oneChecklist(tx *gorm.DB, taskID, checklistID uint) (*domain.Checklist, error) {
	var rows []checklistRow
	if err := tx.Raw(
		"SELECT "+checklistColumns+" FROM task_checklists WHERE id = ? AND task_id = ?", checklistID, taskID,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	lists, err := loadChecklists(tx, rows)
	if err != nil {
		return nil, err
	}
	return &lists[0], nil
}

func oneItem(tx *gorm.DB, taskID, checklistID, itemID uint) (*domain.ChecklistItem, error) {
	var rows []checklistItemRow
	if err := tx.Raw(
		"SELECT i.id, i.created_at, i.updated_at, i.checklist_id, i.text, i.resolved, i.resolved_at, i.resolved_by, "+
			"i.assignee_id, i.position FROM task_checklist_items i "+
			"JOIN task_checklists c ON c.id = i.checklist_id "+
			"WHERE i.id = ? AND i.checklist_id = ? AND c.task_id = ?", itemID, checklistID, taskID,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	items, err := hydrateItems(tx, rows)
	if err != nil {
		return nil, err
	}
	return &items[0], nil
}

func sameIDSet(existing, requested []uint) bool {
	if len(existing) != len(requested) {
		return false
	}
	a := append([]uint(nil), existing...)
	b := append([]uint(nil), requested...)
	sort.Slice(a, func(i, j int) bool { return a[i] < a[j] })
	sort.Slice(b, func(i, j int) bool { return b[i] < b[j] })
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func applyOrder(tx *gorm.DB, table string, existing, ids []uint) error {
	if !sameIDSet(existing, ids) {
		return domain.ErrInvalidChecklistOrder
	}
	for i, id := range ids {
		if err := tx.Exec("UPDATE "+table+" SET position = ? WHERE id = ?", int64(i+1)*positionStep, id).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *checklistRepository) CreateChecklist(ctx context.Context, taskID uint, name string) (*domain.Checklist, error) {
	var created *domain.Checklist
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := lockTask(tx, taskID); err != nil {
			return err
		}
		var count int64
		if err := tx.Raw("SELECT count(*) FROM task_checklists WHERE task_id = ?", taskID).Scan(&count).Error; err != nil {
			return err
		}
		if count >= domain.MaxChecklistsPerTask {
			return domain.ErrTooManyChecklists
		}
		position, err := nextChecklistPosition(tx, "task_checklists", "task_id", taskID)
		if err != nil {
			return err
		}
		var rows []checklistRow
		if err := tx.Raw(
			"INSERT INTO task_checklists (task_id, name, position) VALUES (?, ?, ?) RETURNING "+checklistColumns,
			taskID, name, position,
		).Scan(&rows).Error; err != nil {
			return err
		}
		lists, err := loadChecklists(tx, rows)
		if err != nil {
			return err
		}
		created = &lists[0]
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return created, nil
}

func (r *checklistRepository) ListByTask(ctx context.Context, taskID uint) ([]domain.Checklist, error) {
	db := database.DBFromContext(ctx, r.db)
	var rows []checklistRow
	if err := db.Raw(
		"SELECT "+checklistColumns+" FROM task_checklists WHERE task_id = ? ORDER BY position, id", taskID,
	).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	lists, err := loadChecklists(db, rows)
	return lists, mapErr(err)
}

func (r *checklistRepository) GetChecklist(ctx context.Context, taskID, checklistID uint) (*domain.Checklist, error) {
	list, err := oneChecklist(database.DBFromContext(ctx, r.db), taskID, checklistID)
	return list, mapErr(err)
}

func (r *checklistRepository) RenameChecklist(ctx context.Context, taskID, checklistID uint, name string) (*domain.Checklist, error) {
	var renamed *domain.Checklist
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := lockChecklist(tx, taskID, checklistID); err != nil {
			return err
		}
		if err := tx.Exec("UPDATE task_checklists SET name = ? WHERE id = ?", name, checklistID).Error; err != nil {
			return err
		}
		list, err := oneChecklist(tx, taskID, checklistID)
		renamed = list
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return renamed, nil
}

func (r *checklistRepository) DeleteChecklist(ctx context.Context, taskID, checklistID uint) error {
	res := database.DBFromContext(ctx, r.db).Exec("DELETE FROM task_checklists WHERE id = ? AND task_id = ?", checklistID, taskID)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *checklistRepository) ReorderChecklists(ctx context.Context, taskID uint, ids []uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := lockTask(tx, taskID); err != nil {
			return err
		}
		var existing []uint
		if err := tx.Raw("SELECT id FROM task_checklists WHERE task_id = ?", taskID).Scan(&existing).Error; err != nil {
			return err
		}
		return applyOrder(tx, "task_checklists", existing, ids)
	}))
}

func validateItemAssignee(tx *gorm.DB, taskID, userID uint) error {
	var count int64
	err := tx.Raw(
		"SELECT count(*) FROM tasks t JOIN spaces s ON s.id = t.space_id "+
			"JOIN workspace_members wm ON wm.workspace_id = s.workspace_id AND wm.deleted_at IS NULL "+
			"WHERE t.id = ? AND wm.user_id = ?", taskID, userID,
	).Scan(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		return domain.ErrInvalidAssigneeReference
	}
	return nil
}

func (r *checklistRepository) CreateItem(ctx context.Context, taskID, checklistID uint, text string, assigneeID *uint) (*domain.ChecklistItem, error) {
	var created *domain.ChecklistItem
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := lockChecklist(tx, taskID, checklistID); err != nil {
			return err
		}
		if assigneeID != nil {
			if err := validateItemAssignee(tx, taskID, *assigneeID); err != nil {
				return err
			}
		}
		var count int64
		if err := tx.Raw("SELECT count(*) FROM task_checklist_items WHERE checklist_id = ?", checklistID).Scan(&count).Error; err != nil {
			return err
		}
		if count >= domain.MaxItemsPerChecklist {
			return domain.ErrTooManyChecklistItems
		}
		position, err := nextChecklistPosition(tx, "task_checklist_items", "checklist_id", checklistID)
		if err != nil {
			return err
		}
		var ids []uint
		if err := tx.Raw(
			"INSERT INTO task_checklist_items (checklist_id, text, assignee_id, position) VALUES (?, ?, ?, ?) RETURNING id",
			checklistID, text, assigneeID, position,
		).Scan(&ids).Error; err != nil {
			return err
		}
		item, err := oneItem(tx, taskID, checklistID, ids[0])
		created = item
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return created, nil
}

func (r *checklistRepository) GetItem(ctx context.Context, taskID, checklistID, itemID uint) (*domain.ChecklistItem, error) {
	item, err := oneItem(database.DBFromContext(ctx, r.db), taskID, checklistID, itemID)
	return item, mapErr(err)
}

func (r *checklistRepository) UpdateItem(
	ctx context.Context, taskID, checklistID, itemID, actorID uint, update domain.ChecklistItemUpdate,
) (*domain.ChecklistItem, error) {
	var updated *domain.ChecklistItem
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := lockChecklist(tx, taskID, checklistID); err != nil {
			return err
		}
		current, err := oneItem(tx, taskID, checklistID, itemID)
		if err != nil {
			return err
		}

		changes := map[string]any{}
		if update.Text != nil {
			changes["text"] = *update.Text
		}
		if update.Assignee.Set {
			if update.Assignee.Value != nil {
				if err := validateItemAssignee(tx, taskID, *update.Assignee.Value); err != nil {
					return err
				}
			}
			changes["assignee_id"] = update.Assignee.Value
		}
		if update.Resolved != nil && *update.Resolved != current.Resolved {
			changes["resolved"] = *update.Resolved
			if *update.Resolved {
				changes["resolved_at"] = gorm.Expr("now()")
				changes["resolved_by"] = actorID
			} else {
				changes["resolved_at"] = nil
				changes["resolved_by"] = nil
			}
		}
		if len(changes) > 0 {
			if err := tx.Table("task_checklist_items").Where("id = ?", itemID).Updates(changes).Error; err != nil {
				return err
			}
		}
		item, err := oneItem(tx, taskID, checklistID, itemID)
		updated = item
		return err
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return updated, nil
}

func (r *checklistRepository) DeleteItem(ctx context.Context, taskID, checklistID, itemID uint) error {
	res := database.DBFromContext(ctx, r.db).Exec(
		"DELETE FROM task_checklist_items i USING task_checklists c "+
			"WHERE i.id = ? AND i.checklist_id = ? AND c.id = i.checklist_id AND c.task_id = ?",
		itemID, checklistID, taskID,
	)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *checklistRepository) ReorderItems(ctx context.Context, taskID, checklistID uint, ids []uint) error {
	return mapErr(database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := lockChecklist(tx, taskID, checklistID); err != nil {
			return err
		}
		var existing []uint
		if err := tx.Raw("SELECT id FROM task_checklist_items WHERE checklist_id = ?", checklistID).Scan(&existing).Error; err != nil {
			return err
		}
		return applyOrder(tx, "task_checklist_items", existing, ids)
	}))
}
