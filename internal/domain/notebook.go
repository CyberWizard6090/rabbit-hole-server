package domain

import (
	"context"
	"errors"
	"time"
)

const (
	MaxNotebookNameRunes        = 255
	MaxNotebookDescriptionRunes = 2000
	MaxNotebooksPerWorkspace    = 200
	MaxDocumentTitleRunes       = 255
	MaxDocumentsPerNotebook     = 5000

	// MaxDocumentUpdateBytes bounds one Yjs update. A client sends larger changes as several updates.
	MaxDocumentUpdateBytes = 512 * 1024
	// MaxDocumentStateBytes bounds the compacted state of one document.
	MaxDocumentStateBytes = 8 * 1024 * 1024
	// MaxDocumentPendingUpdates is how many updates may follow a snapshot before the server asks for compaction.
	MaxDocumentPendingUpdates = 500
	// MaxTouchedBlocks is how many block ids one update may report.
	MaxTouchedBlocks  = 200
	MaxBlockIDRunes   = 100
	MaxActivityBlocks = 500
)

var (
	ErrNotebookNotFound      = errors.New("notebook not found")
	ErrNotebookLimitReached  = errors.New("too many notebooks")
	ErrInvalidNotebook       = errors.New("invalid notebook")
	ErrDocumentNotFound      = errors.New("document not found")
	ErrDocumentLimitReached  = errors.New("too many documents")
	ErrInvalidDocument       = errors.New("invalid document")
	ErrInvalidDocumentUpdate = errors.New("invalid document update")
	ErrUpdateTooLarge        = errors.New("document update too large")
	ErrStateTooLarge         = errors.New("document state too large")
	ErrSnapshotConflict      = errors.New("snapshot does not match the document history")
)

type Notebook struct {
	ID            uint         `json:"id"`
	WorkspaceID   uint         `json:"workspace_id"`
	Name          string       `json:"name"`
	Description   *string      `json:"description"`
	Position      int64        `json:"position"`
	DocumentCount int64        `json:"document_count"`
	CreatedBy     *CommentUser `json:"created_by"`
	CreatedAt     time.Time    `json:"created_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
}

type NotebookDraft struct {
	WorkspaceID uint
	UserID      uint
	Name        string
	Description *string
}

type NotebookUpdate struct {
	Name        *string
	Description Nullable[string]
	Position    *int64
}

type Document struct {
	ID           uint         `json:"id"`
	WorkspaceID  uint         `json:"workspace_id"`
	NotebookID   uint         `json:"notebook_id"`
	Title        string       `json:"title"`
	Position     int64        `json:"position"`
	LastSeq      int64        `json:"last_seq"`
	CreatedBy    *CommentUser `json:"created_by"`
	LastEditedBy *CommentUser `json:"last_edited_by"`
	LastEditedAt *time.Time   `json:"last_edited_at"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
}

type DocumentDraft struct {
	NotebookID uint
	UserID     uint
	Title      string
}

type DocumentUpdate struct {
	Title      *string
	Position   *int64
	NotebookID *uint
}

// DocumentState is what a client needs to open a document: the compacted state and the updates after it.
// Applying them in order (Y.applyUpdate) gives the current document.
type DocumentState struct {
	Format      int              `json:"format"`
	SnapshotSeq int64            `json:"snapshot_seq"`
	Snapshot    []byte           `json:"snapshot"`
	Updates     []DocumentChange `json:"updates"`
	LastSeq     int64            `json:"last_seq"`
	// NeedsCompaction tells a client to send a fresh snapshot: too many updates follow the last one.
	NeedsCompaction bool `json:"needs_compaction"`
}

type DocumentChange struct {
	Seq       int64     `json:"seq"`
	Update    []byte    `json:"update"`
	CreatedAt time.Time `json:"created_at"`
	// UserID is the author of the update; the API shows only public user fields through Author.
	UserID uint         `json:"-"`
	Author *CommentUser `json:"author"`
}

// BlockActivity says who created a block and who edited it last.
type BlockActivity struct {
	BlockID      string       `json:"block_id"`
	CreatedBy    *CommentUser `json:"created_by"`
	CreatedAt    time.Time    `json:"created_at"`
	LastEditedBy *CommentUser `json:"last_edited_by"`
	LastEditedAt time.Time    `json:"last_edited_at"`
}

type NotebookRepository interface {
	Create(ctx context.Context, draft NotebookDraft) (*Notebook, error)
	Get(ctx context.Context, id uint) (*Notebook, error)
	List(ctx context.Context, workspaceID uint) ([]Notebook, error)
	Update(ctx context.Context, id uint, update NotebookUpdate) (*Notebook, error)
	Delete(ctx context.Context, id uint) error
	CountInWorkspace(ctx context.Context, workspaceID uint) (int64, error)
}

type DocumentRepository interface {
	// Create inserts the document with the initial Yjs state as its snapshot.
	Create(ctx context.Context, draft DocumentDraft, initialState []byte) (*Document, error)
	Get(ctx context.Context, id uint) (*Document, error)
	List(ctx context.Context, notebookID uint, offset, limit int) ([]Document, int64, error)
	Update(ctx context.Context, id uint, update DocumentUpdate) (*Document, error)
	Delete(ctx context.Context, id uint) error
	CountInNotebook(ctx context.Context, notebookID uint) (int64, error)
	// WorkspaceOf returns the workspace of a live notebook.
	NotebookWorkspace(ctx context.Context, notebookID uint) (uint, error)

	State(ctx context.Context, documentID uint) (*DocumentState, error)
	// AppendUpdate stores one update, records the touched blocks and returns the update number.
	AppendUpdate(ctx context.Context, documentID, userID uint, payload []byte, touched []string) (int64, error)
	// ReplaceSnapshot stores a compacted state that covers updates up to uptoSeq and drops them.
	ReplaceSnapshot(ctx context.Context, documentID uint, uptoSeq int64, state []byte) error
	BlockActivity(ctx context.Context, documentID uint, blockID string, limit int) ([]BlockActivity, error)
}

// DocumentEditingStats is for tests and metrics.
type DocumentEditingStats struct {
	Updates int64
}
