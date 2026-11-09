package repository

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type savedViewRepository struct {
	db *gorm.DB
}

func NewSavedViewRepository(db *gorm.DB) domain.SavedViewRepository {
	return &savedViewRepository{db: db}
}

type savedViewRow struct {
	ID          uint
	WorkspaceID uint
	ListID      *uint
	OwnerID     uint
	Name        string
	Shared      bool
	Filters     []byte
	Sort        string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const savedViewColumns = "v.id, v.workspace_id, v.list_id, v.owner_id, v.name, v.shared, v.filters, v.sort, v.created_at, v.updated_at"

func (r *savedViewRepository) present(db *gorm.DB, rows []savedViewRow, viewerID uint) ([]domain.SavedView, error) {
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.OwnerID)
	}
	owners, err := loadPublicUsers(db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.SavedView, 0, len(rows))
	for _, row := range rows {
		filters := map[string]string{}
		if len(row.Filters) > 0 {
			if err := json.Unmarshal(row.Filters, &filters); err != nil {
				return nil, err
			}
		}
		view := domain.SavedView{
			ID: row.ID, WorkspaceID: row.WorkspaceID, ListID: row.ListID, OwnerID: row.OwnerID, IsOwner: row.OwnerID == viewerID,
			Name: row.Name, Shared: row.Shared, Filters: filters, Sort: row.Sort, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if owner, ok := owners[row.OwnerID]; ok {
			view.Owner = &owner
		}
		out = append(out, view)
	}
	return out, nil
}

func (r *savedViewRepository) one(ctx context.Context, workspaceID, id, viewerID uint) (*domain.SavedView, error) {
	db := database.DBFromContext(ctx, r.db)
	var rows []savedViewRow
	if err := db.Raw("SELECT "+savedViewColumns+" FROM saved_views v WHERE v.id = ? AND v.workspace_id = ?", id, workspaceID).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrViewNotFound
	}
	views, err := r.present(db, rows, viewerID)
	if err != nil {
		return nil, mapErr(err)
	}
	return &views[0], nil
}

func filtersJSON(filters map[string]string) (string, error) {
	if filters == nil {
		filters = map[string]string{}
	}
	raw, err := json.Marshal(filters)
	return string(raw), err
}

func (r *savedViewRepository) Create(ctx context.Context, draft domain.SavedViewDraft) (*domain.SavedView, error) {
	filters, err := filtersJSON(draft.Filters)
	if err != nil {
		return nil, err
	}
	var id uint
	if err := database.DBFromContext(ctx, r.db).Raw(
		`INSERT INTO saved_views (workspace_id, list_id, owner_id, name, shared, filters, sort)
		 VALUES (?, ?, ?, ?, ?, ?::jsonb, ?) RETURNING id`,
		draft.WorkspaceID, draft.ListID, draft.OwnerID, draft.Name, draft.Shared, filters, draft.Sort,
	).Scan(&id).Error; err != nil {
		return nil, mapErr(err)
	}
	return r.one(ctx, draft.WorkspaceID, id, draft.OwnerID)
}

func (r *savedViewRepository) Get(ctx context.Context, workspaceID, id uint) (*domain.SavedView, error) {
	return r.one(ctx, workspaceID, id, 0)
}

func (r *savedViewRepository) List(ctx context.Context, workspaceID, viewerID uint, listID *uint) ([]domain.SavedView, error) {
	db := database.DBFromContext(ctx, r.db)
	query := "SELECT " + savedViewColumns + ` FROM saved_views v
		LEFT JOIN lists l ON l.id = v.list_id
		WHERE v.workspace_id = ? AND (v.shared OR v.owner_id = ?) AND (v.list_id IS NULL OR l.deleted_at IS NULL)`
	args := []any{workspaceID, viewerID}
	if listID != nil {
		query += " AND (v.list_id = ? OR v.list_id IS NULL)"
		args = append(args, *listID)
	}
	query += " ORDER BY v.shared DESC, lower(v.name), v.id"
	var rows []savedViewRow
	if err := db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	views, err := r.present(db, rows, viewerID)
	return views, mapErr(err)
}

func (r *savedViewRepository) Update(ctx context.Context, workspaceID, id uint, update domain.SavedViewUpdate) (*domain.SavedView, error) {
	sets := []string{"updated_at = now()"}
	args := []any{}
	if update.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *update.Name)
	}
	if update.Shared != nil {
		sets = append(sets, "shared = ?")
		args = append(args, *update.Shared)
	}
	if update.Filters != nil {
		raw, err := filtersJSON(update.Filters)
		if err != nil {
			return nil, err
		}
		sets = append(sets, "filters = ?::jsonb")
		args = append(args, raw)
	}
	if update.Sort != nil {
		sets = append(sets, "sort = ?")
		args = append(args, *update.Sort)
	}
	query := "UPDATE saved_views SET "
	for i, set := range sets {
		if i > 0 {
			query += ", "
		}
		query += set
	}
	query += " WHERE id = ? AND workspace_id = ?"
	res := database.DBFromContext(ctx, r.db).Exec(query, append(args, id, workspaceID)...)
	if res.Error != nil {
		return nil, mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, domain.ErrViewNotFound
	}
	return r.one(ctx, workspaceID, id, 0)
}

func (r *savedViewRepository) Delete(ctx context.Context, workspaceID, id uint) error {
	res := database.DBFromContext(ctx, r.db).Exec("DELETE FROM saved_views WHERE id = ? AND workspace_id = ?", id, workspaceID)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrViewNotFound
	}
	return nil
}

func (r *savedViewRepository) CountOwned(ctx context.Context, workspaceID, ownerID uint) (int64, error) {
	var n int64
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT count(*) FROM saved_views WHERE workspace_id = ? AND owner_id = ?", workspaceID, ownerID).Scan(&n).Error
	return n, mapErr(err)
}

func (r *savedViewRepository) ListBelongsToWorkspace(ctx context.Context, workspaceID, listID uint) (bool, error) {
	var found bool
	err := database.DBFromContext(ctx, r.db).Raw(
		`SELECT EXISTS (SELECT 1 FROM lists l JOIN spaces s ON s.id = l.space_id AND s.deleted_at IS NULL
		 WHERE l.id = ? AND l.deleted_at IS NULL AND s.workspace_id = ?)`, listID, workspaceID).Scan(&found).Error
	return found, mapErr(err)
}
