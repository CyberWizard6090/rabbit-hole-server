package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/database"
	"rabbit-hole-server/internal/domain"
)

const formUploadColumns = "id, public_id, form_id, question_id, storage_key, name, content_type, size, " +
	"created_at, expires_at, claimed_at, file_id"

type formUploadRepository struct {
	db *gorm.DB
}

func NewFormUploadRepository(db *gorm.DB) domain.FormUploadRepository {
	return &formUploadRepository{db: db}
}

type formUploadRow struct {
	ID          uint
	PublicID    string
	FormID      uint
	QuestionID  *uint
	StorageKey  string
	Name        string
	ContentType string
	Size        int64
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ClaimedAt   *time.Time
	FileID      *uint
}

func (row formUploadRow) toDomain() domain.FormUpload {
	return domain.FormUpload{
		ID: row.ID, PublicID: row.PublicID, FormID: row.FormID, QuestionID: row.QuestionID,
		StorageKey: row.StorageKey, Name: row.Name, ContentType: row.ContentType, Size: row.Size,
		CreatedAt: row.CreatedAt, ExpiresAt: row.ExpiresAt, ClaimedAt: row.ClaimedAt, FileID: row.FileID,
	}
}

func (r *formUploadRepository) Create(ctx context.Context, upload domain.NewFormUpload) (*domain.FormUpload, error) {
	var created domain.FormUpload
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var pending int64
		if err := tx.Raw(
			"SELECT count(*) FROM form_uploads WHERE form_id = ? AND claimed_at IS NULL AND expires_at > now()",
			upload.FormID,
		).Scan(&pending).Error; err != nil {
			return err
		}
		if pending >= domain.MaxPendingFormUploads {
			return domain.ErrTooManyPendingUploads
		}
		var row formUploadRow
		if err := tx.Raw(
			"INSERT INTO form_uploads (public_id, form_id, storage_key, name, content_type, size, expires_at) "+
				"VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING "+formUploadColumns,
			upload.PublicID, upload.FormID, upload.StorageKey, upload.Name, upload.ContentType, upload.Size,
			upload.ExpiresAt,
		).Scan(&row).Error; err != nil {
			return err
		}
		created = row.toDomain()
		return nil
	})
	if err != nil {
		return nil, mapErr(err)
	}
	return &created, nil
}

func (r *formUploadRepository) ListExpired(ctx context.Context, now time.Time, limit int) ([]domain.ExpiredFormUpload, error) {
	var rows []struct {
		ID         uint
		StorageKey string
	}
	if err := r.db.WithContext(ctx).Raw(
		"SELECT id, storage_key FROM form_uploads WHERE claimed_at IS NULL AND expires_at <= ? ORDER BY id LIMIT ?",
		now, limit,
	).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	out := make([]domain.ExpiredFormUpload, len(rows))
	for i, row := range rows {
		out[i] = domain.ExpiredFormUpload{ID: row.ID, StorageKey: row.StorageKey}
	}
	return out, nil
}

func (r *formUploadRepository) DeleteExpired(ctx context.Context, now time.Time, ids []uint) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := r.db.WithContext(ctx).Exec(
		"DELETE FROM form_uploads WHERE id IN ? AND claimed_at IS NULL AND expires_at <= ?", ids, now,
	)
	return res.RowsAffected, mapErr(res.Error)
}

func (r *formUploadRepository) GetForSubmission(
	ctx context.Context, workspaceID, submissionID, uploadID uint,
) (*domain.FormUpload, error) {
	var rows []formUploadRow
	if err := r.db.WithContext(ctx).Raw(
		"SELECT fu.id, fu.public_id, fu.form_id, fu.question_id, fu.storage_key, fu.name, fu.content_type, fu.size, "+
			"fu.created_at, fu.expires_at, fu.claimed_at, fu.file_id FROM form_uploads fu "+
			"JOIN form_submissions fs ON fs.id = fu.submission_id "+
			"WHERE fu.id = ? AND fu.submission_id = ? AND fs.workspace_id = ?",
		uploadID, submissionID, workspaceID,
	).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	if len(rows) == 0 {
		return nil, domain.ErrNotFound
	}
	upload := rows[0].toDomain()
	return &upload, nil
}

func (r *formUploadRepository) ListSubmissionFiles(
	ctx context.Context, submissionIDs []uint,
) (map[uint][]domain.SubmissionFile, error) {
	out := make(map[uint][]domain.SubmissionFile, len(submissionIDs))
	if len(submissionIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		ID           uint
		SubmissionID uint
		QuestionID   *uint
		Name         string
		ContentType  string
		Size         int64
		FileID       *uint
	}
	if err := r.db.WithContext(ctx).Raw(
		"SELECT id, submission_id, question_id, name, content_type, size, file_id FROM form_uploads "+
			"WHERE submission_id IN ? ORDER BY id", submissionIDs,
	).Scan(&rows).Error; err != nil {
		return nil, mapErr(err)
	}
	for _, row := range rows {
		file := domain.SubmissionFile{
			ID: row.ID, Name: row.Name, ContentType: row.ContentType, Size: row.Size, TaskAttachmentID: row.FileID,
		}
		if row.QuestionID != nil {
			file.QuestionID = *row.QuestionID
		}
		out[row.SubmissionID] = append(out[row.SubmissionID], file)
	}
	return out, nil
}

func (r *formUploadRepository) AttachSubmissionFiles(
	ctx context.Context, submissionID, taskID, userID uint,
) ([]domain.File, error) {
	tx := database.DBFromContext(ctx, r.db)
	var uploads []formUploadRow
	if err := tx.Raw(
		"SELECT "+formUploadColumns+" FROM form_uploads WHERE submission_id = ? AND file_id IS NULL "+
			"ORDER BY id FOR UPDATE", submissionID,
	).Scan(&uploads).Error; err != nil {
		return nil, mapErr(err)
	}
	created := make([]fileRow, 0, len(uploads))
	for _, upload := range uploads {
		row, err := insertFile(tx, domain.NewFile{
			Kind: domain.FileKindAttachment, PublicID: upload.PublicID, StorageKey: upload.StorageKey,
			UserID: userID, TaskID: &taskID, Name: upload.Name, ContentType: upload.ContentType, Size: upload.Size,
		})
		if err != nil {
			return nil, mapErr(err)
		}
		if err := tx.Exec("UPDATE form_uploads SET file_id = ? WHERE id = ?", row.ID, upload.ID).Error; err != nil {
			return nil, mapErr(err)
		}
		created = append(created, row)
	}
	files, err := hydrateFiles(tx, created)
	return files, mapErr(err)
}

func claimFormUploads(
	tx *gorm.DB, formID, submissionID uint, claims []domain.FormUploadClaim, now time.Time,
) error {
	for _, claim := range claims {
		res := tx.Exec(
			"UPDATE form_uploads SET claimed_at = ?, submission_id = ?, question_id = ? "+
				"WHERE form_id = ? AND public_id = ? AND claimed_at IS NULL AND expires_at > ?",
			now, submissionID, claim.QuestionID, formID, claim.PublicID, now,
		)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 1 {
			return domain.NewFormError(domain.FormUploadInvalidCode,
				"an uploaded file is unknown, expired, already used or belongs to another form")
		}
	}
	return nil
}
