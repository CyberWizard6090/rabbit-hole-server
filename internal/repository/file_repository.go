package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

type fileRepository struct {
	db *gorm.DB
}

func NewFileRepository(db *gorm.DB) domain.FileRepository {
	return &fileRepository{db: db}
}

type fileRow struct {
	ID          uint
	CreatedAt   time.Time
	Kind        string
	PublicID    string
	StorageKey  string
	UserID      uint
	TaskID      *uint
	Name        string
	ContentType string
	Size        int64
}

const fileColumns = "id, created_at, kind, public_id, storage_key, user_id, task_id, name, content_type, size"

func insertFile(tx *gorm.DB, f domain.NewFile) (fileRow, error) {
	var row fileRow
	err := tx.Raw(
		"INSERT INTO files (kind, public_id, storage_key, user_id, workspace_id, task_id, name, content_type, size) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING "+fileColumns,
		f.Kind, f.PublicID, f.StorageKey, f.UserID, f.WorkspaceID, f.TaskID, f.Name, f.ContentType, f.Size,
	).Scan(&row).Error
	return row, err
}

func hydrateFiles(tx *gorm.DB, rows []fileRow) ([]domain.File, error) {
	if len(rows) == 0 {
		return []domain.File{}, nil
	}
	ids := make([]uint, len(rows))
	for i, row := range rows {
		ids[i] = row.UserID
	}
	var users []commentUserRow
	if err := tx.Table("users").Select("id, username, first_name, last_name, avatar_url").
		Where("id IN ?", uniqueIDs(ids)).Scan(&users).Error; err != nil {
		return nil, err
	}
	byID := make(map[uint]commentUserRow, len(users))
	for _, u := range users {
		byID[u.ID] = u
	}
	out := make([]domain.File, len(rows))
	for i, row := range rows {
		out[i] = domain.File{
			ID: row.ID, TaskID: row.TaskID, Uploader: byID[row.UserID].toDomain(), Name: row.Name,
			ContentType: row.ContentType, Size: row.Size, CreatedAt: row.CreatedAt,
			Kind: row.Kind, PublicID: row.PublicID, StorageKey: row.StorageKey,
		}
	}
	return out, nil
}

func oneFile(tx *gorm.DB, query string, args ...any) (*domain.File, error) {
	var rows []fileRow
	if err := tx.Raw(query, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	files, err := hydrateFiles(tx, rows)
	if err != nil {
		return nil, err
	}
	return &files[0], nil
}

func (r *fileRepository) Create(ctx context.Context, file domain.NewFile) (*domain.File, error) {
	row, err := insertFile(database.DBFromContext(ctx, r.db), file)
	if err != nil {
		return nil, mapErr(err)
	}
	files, err := hydrateFiles(database.DBFromContext(ctx, r.db), []fileRow{row})
	if err != nil {
		return nil, mapErr(err)
	}
	return &files[0], nil
}

func (r *fileRepository) CreateAttachment(ctx context.Context, file domain.NewFile) (*domain.File, error) {
	if file.TaskID == nil {
		return nil, domain.ErrNotFound
	}
	var created *domain.File
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var taskIDs []uint
		if err := tx.Raw(
			"SELECT id FROM tasks WHERE id = ? AND deleted_at IS NULL FOR UPDATE", *file.TaskID,
		).Scan(&taskIDs).Error; err != nil {
			return err
		}
		if len(taskIDs) == 0 {
			return domain.ErrNotFound
		}

		var count int64
		if err := tx.Raw(
			"SELECT count(*) FROM files WHERE kind = 'attachment' AND task_id = ?", *file.TaskID,
		).Scan(&count).Error; err != nil {
			return err
		}
		if count >= domain.MaxAttachmentsPerTask {
			return domain.ErrTooManyAttachments
		}

		row, err := insertFile(tx, file)
		if err != nil {
			return err
		}
		files, err := hydrateFiles(tx, []fileRow{row})
		if err != nil {
			return err
		}
		created = &files[0]
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return created, nil
}

func (r *fileRepository) ListAttachments(ctx context.Context, taskID uint) ([]domain.File, error) {
	db := database.DBFromContext(ctx, r.db)
	var rows []fileRow
	if err := db.Raw(
		"SELECT "+fileColumns+" FROM files WHERE kind = 'attachment' AND task_id = ? ORDER BY id ASC", taskID,
	).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	files, err := hydrateFiles(db, rows)
	return files, mapErr(err)
}

func (r *fileRepository) GetAttachment(ctx context.Context, taskID, id uint) (*domain.File, error) {
	file, err := oneFile(database.DBFromContext(ctx, r.db),
		"SELECT "+fileColumns+" FROM files WHERE kind = 'attachment' AND task_id = ? AND id = ?", taskID, id)
	return file, mapErr(err)
}

func (r *fileRepository) GetPublic(ctx context.Context, publicID string) (*domain.File, error) {
	file, err := oneFile(database.DBFromContext(ctx, r.db),
		"SELECT "+fileColumns+" FROM files WHERE kind IN ('image', 'avatar') AND public_id = ? AND "+
			"(workspace_id IS NULL OR "+liveWorkspacePredicate("files.workspace_id")+")", publicID)
	return file, mapErr(err)
}

func (r *fileRepository) Delete(ctx context.Context, id uint) error {
	res := database.DBFromContext(ctx, r.db).Exec("DELETE FROM files WHERE id = ?", id)
	if res.Error != nil {
		return mapErr(res.Error)
	}
	if res.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func takeAvatars(tx *gorm.DB, userID uint) ([]domain.File, error) {
	var rows []fileRow
	if err := tx.Raw(
		"DELETE FROM files WHERE kind = 'avatar' AND user_id = ? RETURNING "+fileColumns, userID,
	).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return hydrateFiles(tx, rows)
}

func (r *fileRepository) ReplaceAvatar(ctx context.Context, file domain.NewFile, avatarURL string) (*domain.File, []domain.File, error) {
	var (
		created  *domain.File
		replaced []domain.File
	)
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var err error
		if replaced, err = takeAvatars(tx, file.UserID); err != nil {
			return err
		}
		row, err := insertFile(tx, file)
		if err != nil {
			return err
		}
		files, err := hydrateFiles(tx, []fileRow{row})
		if err != nil {
			return err
		}
		created = &files[0]
		return tx.Exec("UPDATE users SET avatar_url = ? WHERE id = ?", avatarURL, file.UserID).Error
	})
	if err != nil {
		return nil, nil, mapErr(err)
	}
	return created, replaced, nil
}

func (r *fileRepository) ClearAvatar(ctx context.Context, userID uint) ([]domain.File, error) {
	var replaced []domain.File
	err := database.DBFromContext(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		var err error
		if replaced, err = takeAvatars(tx, userID); err != nil {
			return err
		}
		return tx.Exec("UPDATE users SET avatar_url = '' WHERE id = ?", userID).Error
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return replaced, nil
}
