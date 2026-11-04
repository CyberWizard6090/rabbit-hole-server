package domain

import (
	"context"
	"errors"
	"time"
)

const (
	FileKindAttachment = "attachment"
	FileKindImage      = "image"
	FileKindAvatar     = "avatar"

	MaxAttachmentsPerTask = 100
)

var (
	ErrFileTooLarge        = errors.New("file is larger than the allowed size")
	ErrEmptyFile           = errors.New("file is empty")
	ErrUnsupportedFileType = errors.New("file type is not supported")
	ErrInvalidImage        = errors.New("file is not a valid image")
	ErrTooManyAttachments  = errors.New("task already has the maximum number of attachments")
	ErrFileForbidden       = errors.New("only the uploader or a user who can delete tasks may delete this file")
)

type File struct {
	ID          uint        `json:"id"`
	TaskID      *uint       `json:"task_id,omitempty"`
	Uploader    CommentUser `json:"uploader"`
	Name        string      `json:"name"`
	ContentType string      `json:"content_type"`
	Size        int64       `json:"size"`
	CreatedAt   time.Time   `json:"created_at"`

	Kind       string `json:"-"`
	PublicID   string `json:"-"`
	StorageKey string `json:"-"`
}

type NewFile struct {
	Kind        string
	PublicID    string
	StorageKey  string
	UserID      uint
	WorkspaceID *uint
	TaskID      *uint
	Name        string
	ContentType string
	Size        int64
}

type FileRepository interface {
	Create(ctx context.Context, file NewFile) (*File, error)
	CountAttachments(ctx context.Context, taskID uint) (int64, error)
	ListAttachments(ctx context.Context, taskID uint) ([]File, error)
	GetAttachment(ctx context.Context, taskID, id uint) (*File, error)
	GetPublic(ctx context.Context, publicID string) (*File, error)
	Delete(ctx context.Context, id uint) error
	ReplaceAvatar(ctx context.Context, file NewFile, avatarURL string) (created *File, replaced []File, err error)
	ClearAvatar(ctx context.Context, userID uint) (replaced []File, err error)
}

var safeServedTypes = map[string]struct{}{
	"image/png": {}, "image/jpeg": {}, "image/gif": {}, "image/webp": {}, "image/avif": {},
	"application/pdf": {}, "text/plain; charset=utf-8": {}, "text/plain": {},
}

func ServedContentType(stored string) string {
	if _, ok := safeServedTypes[stored]; ok {
		return stored
	}
	return "application/octet-stream"
}
