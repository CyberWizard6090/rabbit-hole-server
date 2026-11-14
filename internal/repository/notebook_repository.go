package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

// notebookPositionStep leaves room between neighbours so a notebook can be moved without renumbering.
const notebookPositionStep = 1024

type notebookRepository struct {
	db *gorm.DB
}

func NewNotebookRepository(db *gorm.DB) domain.NotebookRepository {
	return &notebookRepository{db: db}
}

type notebookRow struct {
	ID            uint
	WorkspaceID   uint
	Name          string
	Description   *string
	UserID        uint
	Position      int64
	DocumentCount int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const notebookColumns = "n.id, n.workspace_id, n.name, n.description, n.user_id, n.position, n.created_at, n.updated_at, " +
	"(SELECT count(*) FROM documents d WHERE d.notebook_id = n.id AND d.deleted_at IS NULL) AS document_count"

func presentNotebooks(db *gorm.DB, rows []notebookRow) ([]domain.Notebook, error) {
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.UserID)
	}
	users, err := loadPublicUsers(db, ids)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Notebook, 0, len(rows))
	for _, row := range rows {
		notebook := domain.Notebook{
			ID: row.ID, WorkspaceID: row.WorkspaceID, Name: row.Name, Description: row.Description,
			Position: row.Position, DocumentCount: row.DocumentCount, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if user, ok := users[row.UserID]; ok {
			notebook.CreatedBy = &user
		}
		out = append(out, notebook)
	}
	return out, nil
}

func (r *notebookRepository) Create(ctx context.Context, draft domain.NotebookDraft) (*domain.Notebook, error) {
	var id uint
	if err := database.DBFromContext(ctx, r.db).Raw(
		`INSERT INTO notebooks (workspace_id, name, description, user_id, position)
		 VALUES (?, ?, ?, ?, COALESCE((SELECT max(position) FROM notebooks WHERE workspace_id = ? AND deleted_at IS NULL), 0) + ?)
		 RETURNING id`,
		draft.WorkspaceID, draft.Name, draft.Description, draft.UserID, draft.WorkspaceID, notebookPositionStep,
	).Scan(&id).Error; err != nil {
		return nil, mapErr(err)
	}
	return r.Get(ctx, id)
}

func (r *notebookRepository) Get(ctx context.Context, id uint) (*domain.Notebook, error) {
	db := database.DBFromContext(ctx, r.db)
	var rows []notebookRow
	if err := db.Raw("SELECT "+notebookColumns+" FROM notebooks n WHERE n.id = ? AND "+liveNotebookPredicate("n.id"), id).
		Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotebookNotFound
	}
	notebooks, err := presentNotebooks(db, rows)
	if err != nil {
		return nil, mapErr(err)
	}
	return &notebooks[0], nil
}

func (r *notebookRepository) List(ctx context.Context, workspaceID uint) ([]domain.Notebook, error) {
	db := database.DBFromContext(ctx, r.db)
	var rows []notebookRow
	if err := db.Raw(
		"SELECT "+notebookColumns+" FROM notebooks n WHERE n.workspace_id = ? AND n.deleted_at IS NULL AND "+
			liveWorkspacePredicate("n.workspace_id")+" ORDER BY n.position, n.id", workspaceID,
	).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	notebooks, err := presentNotebooks(db, rows)
	return notebooks, mapErr(err)
}

func (r *notebookRepository) Update(ctx context.Context, id uint, update domain.NotebookUpdate) (*domain.Notebook, error) {
	sets := []string{"updated_at = now()"}
	args := []any{}
	if update.Name != nil {
		sets = append(sets, "name = ?")
		args = append(args, *update.Name)
	}
	if update.Description.Set {
		sets = append(sets, "description = ?")
		args = append(args, update.Description.Value)
	}
	if update.Position != nil {
		sets = append(sets, "position = ?")
		args = append(args, *update.Position)
	}
	res := database.DBFromContext(ctx, r.db).Exec(
		"UPDATE notebooks SET "+strings.Join(sets, ", ")+" WHERE id = ? AND deleted_at IS NULL", append(args, id)...)
	if res.Error != nil {
		return nil, mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, domain.ErrNotebookNotFound
	}
	return r.Get(ctx, id)
}

// Delete marks the notebook and its documents deleted with one transaction, like a list and its tasks.
func (r *notebookRepository) Delete(ctx context.Context, id uint) error {
	return database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		res := tx.Exec("UPDATE notebooks SET deleted_at = now() WHERE id = ? AND deleted_at IS NULL", id)
		if res.Error != nil {
			return mapErr(res.Error)
		}
		if res.RowsAffected == 0 {
			return domain.ErrNotebookNotFound
		}
		return mapErr(tx.Exec("UPDATE documents SET deleted_at = now() WHERE notebook_id = ? AND deleted_at IS NULL", id).Error)
	})
}

func (r *notebookRepository) CountInWorkspace(ctx context.Context, workspaceID uint) (int64, error) {
	var n int64
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT count(*) FROM notebooks WHERE workspace_id = ? AND deleted_at IS NULL", workspaceID).Scan(&n).Error
	return n, mapErr(err)
}
