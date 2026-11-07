package domain

import (
	"context"
	"errors"
	"time"
)

type FormUpload struct {
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

type NewFormUpload struct {
	PublicID    string
	FormID      uint
	StorageKey  string
	Name        string
	ContentType string
	Size        int64
	ExpiresAt   time.Time
}

type ExpiredFormUpload struct {
	ID         uint
	StorageKey string
}

const (
	MaxPendingFormUploads = 500
	FormUploadInvalidCode = "FORM_UPLOAD_INVALID"
)

var ErrTooManyPendingUploads = errors.New("too many pending uploads for this form, try again later")

type FormUploadRepository interface {
	Create(ctx context.Context, upload NewFormUpload) (*FormUpload, error)
	ListExpired(ctx context.Context, now time.Time, limit int) ([]ExpiredFormUpload, error)
	DeleteExpired(ctx context.Context, now time.Time, ids []uint) (int64, error)
	GetForSubmission(ctx context.Context, workspaceID, submissionID, uploadID uint) (*FormUpload, error)
	ListSubmissionFiles(ctx context.Context, submissionIDs []uint) (map[uint][]SubmissionFile, error)
	AttachSubmissionFiles(ctx context.Context, submissionID, taskID, userID uint) ([]File, error)
}
