package repository

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/collab"
	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

const documentPositionStep = 1024

type documentRepository struct {
	db *gorm.DB
}

func NewDocumentRepository(db *gorm.DB) domain.DocumentRepository {
	return &documentRepository{db: db}
}

type documentRow struct {
	ID           uint
	WorkspaceID  uint
	NotebookID   uint
	Title        string
	UserID       uint
	Position     int64
	UpdateSeq    int64
	LastEditedBy *uint
	LastEditedAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

const documentColumns = "d.id, d.workspace_id, d.notebook_id, d.title, d.user_id, d.position, d.update_seq, " +
	"d.last_edited_by, d.last_edited_at, d.created_at, d.updated_at"

func presentDocuments(db *gorm.DB, rows []documentRow) ([]domain.Document, error) {
	ids := make([]uint, 0, len(rows)*2)
	for _, row := range rows {
		ids = append(ids, row.UserID)
		if row.LastEditedBy != nil {
			ids = append(ids, *row.LastEditedBy)
		}
	}
	users, err := loadPublicUsers(db, ids)
	if err != nil {
		return nil, err
	}
	person := func(id uint) *domain.CommentUser {
		if user, ok := users[id]; ok {
			return &user
		}
		return nil
	}
	out := make([]domain.Document, 0, len(rows))
	for _, row := range rows {
		document := domain.Document{
			ID: row.ID, WorkspaceID: row.WorkspaceID, NotebookID: row.NotebookID, Title: row.Title,
			Position: row.Position, LastSeq: row.UpdateSeq, CreatedBy: person(row.UserID),
			LastEditedAt: row.LastEditedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		}
		if row.LastEditedBy != nil {
			document.LastEditedBy = person(*row.LastEditedBy)
		}
		out = append(out, document)
	}
	return out, nil
}

func (r *documentRepository) NotebookWorkspace(ctx context.Context, notebookID uint) (uint, error) {
	var ids []uint
	if err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT n.workspace_id FROM notebooks n WHERE n.id = ? AND "+liveNotebookPredicate("n.id"), notebookID,
	).Scan(&ids).Error; err != nil {
		return 0, mapErr(err)
	}
	if len(ids) == 0 {
		return 0, domain.ErrNotebookNotFound
	}
	return ids[0], nil
}

func (r *documentRepository) Create(ctx context.Context, draft domain.DocumentDraft, initialState []byte) (*domain.Document, error) {
	var id uint
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		var workspaces []uint
		// Share-lock the notebook so it cannot be deleted while the document is being added to it.
		if err := tx.Raw(
			"SELECT n.workspace_id FROM notebooks n WHERE n.id = ? AND n.deleted_at IS NULL AND "+
				liveWorkspacePredicate("n.workspace_id")+" FOR KEY SHARE OF n", draft.NotebookID,
		).Scan(&workspaces).Error; err != nil {
			return mapErr(err)
		}
		if len(workspaces) == 0 {
			return domain.ErrNotebookNotFound
		}
		if err := tx.Raw(
			`INSERT INTO documents (workspace_id, notebook_id, title, user_id, position)
			 VALUES (?, ?, ?, ?, COALESCE((SELECT max(position) FROM documents WHERE notebook_id = ? AND deleted_at IS NULL), 0) + ?)
			 RETURNING id`,
			workspaces[0], draft.NotebookID, draft.Title, draft.UserID, draft.NotebookID, documentPositionStep,
		).Scan(&id).Error; err != nil {
			return mapErr(err)
		}
		return mapErr(tx.Exec(
			"INSERT INTO document_snapshots (document_id, upto_seq, state, format) VALUES (?, 0, ?, ?)",
			id, initialState, collab.FormatVersion).Error)
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *documentRepository) Get(ctx context.Context, id uint) (*domain.Document, error) {
	db := database.DBFromContext(ctx, r.db)
	var rows []documentRow
	if err := db.Raw("SELECT "+documentColumns+" FROM documents d WHERE d.id = ? AND "+liveDocumentPredicate("d"), id).
		Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrDocumentNotFound
	}
	documents, err := presentDocuments(db, rows)
	if err != nil {
		return nil, mapErr(err)
	}
	return &documents[0], nil
}

func (r *documentRepository) List(ctx context.Context, notebookID uint, offset, limit int) ([]domain.Document, int64, error) {
	db := database.DBFromContext(ctx, r.db)
	var total int64
	if err := db.Raw(
		"SELECT count(*) FROM documents d WHERE d.notebook_id = ? AND "+liveDocumentPredicate("d"), notebookID,
	).Scan(&total).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	var rows []documentRow
	if err := db.Raw(
		"SELECT "+documentColumns+" FROM documents d WHERE d.notebook_id = ? AND "+liveDocumentPredicate("d")+
			" ORDER BY d.position, d.id LIMIT ? OFFSET ?", notebookID, limit, offset,
	).Scan(&rows).Error; err != nil {
		return nil, 0, mapErr(err)
	}
	documents, err := presentDocuments(db, rows)
	return documents, total, mapErr(err)
}

func (r *documentRepository) Update(ctx context.Context, id uint, update domain.DocumentUpdate) (*domain.Document, error) {
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		sets := []string{"updated_at = now()"}
		args := []any{}
		if update.Title != nil {
			sets = append(sets, "title = ?")
			args = append(args, *update.Title)
		}
		if update.NotebookID != nil {
			// A document can only move to a live notebook of the same workspace.
			var ok bool
			if err := tx.Raw(
				`SELECT EXISTS (SELECT 1 FROM notebooks n JOIN documents d ON d.workspace_id = n.workspace_id
				 WHERE d.id = ? AND n.id = ? AND `+liveNotebookPredicate("n.id")+`) `, id, *update.NotebookID,
			).Scan(&ok).Error; err != nil {
				return mapErr(err)
			}
			if !ok {
				return domain.ErrNotebookNotFound
			}
			sets = append(sets, "notebook_id = ?")
			args = append(args, *update.NotebookID)
			if update.Position == nil {
				sets = append(sets, "position = COALESCE((SELECT max(position) FROM documents WHERE notebook_id = ? AND deleted_at IS NULL), 0) + ?")
				args = append(args, *update.NotebookID, documentPositionStep)
			}
		}
		if update.Position != nil {
			sets = append(sets, "position = ?")
			args = append(args, *update.Position)
		}
		res := tx.Exec("UPDATE documents SET "+strings.Join(sets, ", ")+" WHERE id = ? AND deleted_at IS NULL", append(args, id)...)
		if res.Error != nil {
			return mapErr(res.Error)
		}
		if res.RowsAffected == 0 {
			return domain.ErrDocumentNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *documentRepository) Delete(ctx context.Context, id uint) error {
	res := database.DBFromContext(ctx, r.db).Exec("UPDATE documents SET deleted_at = now() WHERE id = ? AND deleted_at IS NULL", id)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrDocumentNotFound
	}
	return nil
}

func (r *documentRepository) CountInNotebook(ctx context.Context, notebookID uint) (int64, error) {
	var n int64
	err := database.DBFromContext(ctx, r.db).Raw(
		"SELECT count(*) FROM documents WHERE notebook_id = ? AND deleted_at IS NULL", notebookID).Scan(&n).Error
	return n, mapErr(err)
}

func (r *documentRepository) State(ctx context.Context, documentID uint) (*domain.DocumentState, error) {
	db := database.DBFromContext(ctx, r.db)
	var snapshots []struct {
		UptoSeq int64
		State   []byte
		Format  int
	}
	if err := db.Raw(
		`SELECT s.upto_seq, s.state, s.format FROM document_snapshots s JOIN documents d ON d.id = s.document_id
		 WHERE s.document_id = ? AND `+liveDocumentPredicate("d"), documentID,
	).Scan(&snapshots).Error; err != nil {
		return nil, mapErr(err)
	}
	if len(snapshots) == 0 {
		return nil, domain.ErrDocumentNotFound
	}
	var rows []struct {
		Seq       int64
		UserID    uint
		Payload   []byte
		CreatedAt time.Time
	}
	if err := db.Raw(
		"SELECT seq, user_id, payload, created_at FROM document_updates WHERE document_id = ? AND seq > ? ORDER BY seq",
		documentID, snapshots[0].UptoSeq,
	).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	authorIDs := make([]uint, 0, len(rows))
	for _, row := range rows {
		authorIDs = append(authorIDs, row.UserID)
	}
	authors, err := loadPublicUsers(db, authorIDs)
	if err != nil {
		return nil, mapErr(err)
	}
	state := &domain.DocumentState{
		Format:          snapshots[0].Format,
		SnapshotSeq:     snapshots[0].UptoSeq,
		Snapshot:        snapshots[0].State,
		Updates:         make([]domain.DocumentChange, 0, len(rows)),
		LastSeq:         snapshots[0].UptoSeq,
		NeedsCompaction: len(rows) >= domain.MaxDocumentPendingUpdates,
	}
	for _, row := range rows {
		change := domain.DocumentChange{Seq: row.Seq, Update: row.Payload, CreatedAt: row.CreatedAt, UserID: row.UserID}
		if author, ok := authors[row.UserID]; ok {
			change.Author = &author
		}
		state.Updates = append(state.Updates, change)
		state.LastSeq = row.Seq
	}
	return state, nil
}

func (r *documentRepository) AppendUpdate(ctx context.Context, documentID, userID uint, payload []byte, touched []string) (int64, error) {
	var seq int64
	err := database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		// The row lock serializes writers of one document, so update numbers have no gaps and a fixed order.
		var seqs []int64
		if err := tx.Raw(
			`UPDATE documents d SET update_seq = d.update_seq + 1, last_edited_by = ?, last_edited_at = now()
			 WHERE d.id = ? AND `+liveDocumentPredicate("d")+` RETURNING d.update_seq`, userID, documentID,
		).Scan(&seqs).Error; err != nil {
			return mapErr(err)
		}
		if len(seqs) == 0 {
			return domain.ErrDocumentNotFound
		}
		seq = seqs[0]
		if err := tx.Exec(
			"INSERT INTO document_updates (document_id, seq, user_id, payload) VALUES (?, ?, ?, ?)",
			documentID, seq, userID, payload).Error; err != nil {
			return mapErr(err)
		}
		if len(touched) == 0 {
			return nil
		}
		values := make([]string, 0, len(touched))
		args := make([]any, 0, len(touched)*4)
		for _, blockID := range touched {
			values = append(values, "(?, ?, ?, ?)")
			args = append(args, documentID, blockID, userID, userID)
		}
		return mapErr(tx.Exec(
			"INSERT INTO document_block_activity (document_id, block_id, created_by, last_edited_by) VALUES "+
				strings.Join(values, ", ")+
				" ON CONFLICT (document_id, block_id) DO UPDATE SET last_edited_by = EXCLUDED.last_edited_by, last_edited_at = now()",
			args...).Error)
	})
	return seq, err
}

func (r *documentRepository) ReplaceSnapshot(ctx context.Context, documentID uint, uptoSeq int64, state []byte) error {
	return database.WithTransaction(ctx, r.db, func(txCtx context.Context) error {
		tx := database.DBFromContext(txCtx, r.db)
		var rows []struct {
			UpdateSeq int64
			UptoSeq   int64
		}
		if err := tx.Raw(
			`SELECT d.update_seq, s.upto_seq FROM documents d JOIN document_snapshots s ON s.document_id = d.id
			 WHERE d.id = ? AND `+liveDocumentPredicate("d")+` FOR UPDATE OF d`, documentID,
		).Scan(&rows).Error; err != nil {
			return mapErr(err)
		}
		if len(rows) == 0 {
			return domain.ErrDocumentNotFound
		}
		if uptoSeq <= rows[0].UptoSeq || uptoSeq > rows[0].UpdateSeq {
			return domain.ErrSnapshotConflict
		}
		if err := tx.Exec(
			"UPDATE document_snapshots SET upto_seq = ?, state = ?, updated_at = now() WHERE document_id = ?",
			uptoSeq, state, documentID).Error; err != nil {
			return mapErr(err)
		}
		return mapErr(tx.Exec("DELETE FROM document_updates WHERE document_id = ? AND seq <= ?", documentID, uptoSeq).Error)
	})
}

func (r *documentRepository) BlockActivity(ctx context.Context, documentID uint, blockID string, limit int) ([]domain.BlockActivity, error) {
	db := database.DBFromContext(ctx, r.db)
	query := `SELECT a.block_id, a.created_by, a.created_at, a.last_edited_by, a.last_edited_at
		FROM document_block_activity a JOIN documents d ON d.id = a.document_id
		WHERE a.document_id = ? AND ` + liveDocumentPredicate("d")
	args := []any{documentID}
	if blockID != "" {
		query += " AND a.block_id = ?"
		args = append(args, blockID)
	}
	query += " ORDER BY a.last_edited_at DESC, a.block_id LIMIT ?"
	args = append(args, limit)
	var rows []struct {
		BlockID      string
		CreatedBy    uint
		CreatedAt    time.Time
		LastEditedBy uint
		LastEditedAt time.Time
	}
	if err := db.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	ids := make([]uint, 0, len(rows)*2)
	for _, row := range rows {
		ids = append(ids, row.CreatedBy, row.LastEditedBy)
	}
	users, err := loadPublicUsers(db, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	person := func(id uint) *domain.CommentUser {
		if user, ok := users[id]; ok {
			return &user
		}
		return nil
	}
	out := make([]domain.BlockActivity, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.BlockActivity{
			BlockID: row.BlockID, CreatedBy: person(row.CreatedBy), CreatedAt: row.CreatedAt,
			LastEditedBy: person(row.LastEditedBy), LastEditedAt: row.LastEditedAt,
		})
	}
	return out, nil
}

func (r *documentRepository) PublicUser(ctx context.Context, userID uint) (*domain.CommentUser, error) {
	users, err := loadPublicUsers(database.DBFromContext(ctx, r.db), []uint{userID})
	if err != nil {
		return nil, mapErr(err)
	}
	user, ok := users[userID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	return &user, nil
}
