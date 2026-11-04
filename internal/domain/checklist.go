package domain

import (
	"context"
	"errors"
	"time"
)

const (
	MaxChecklistsPerTask = 20
	MaxItemsPerChecklist = 200
)

var (
	ErrTooManyChecklists     = errors.New("task already has the maximum number of checklists")
	ErrTooManyChecklistItems = errors.New("checklist already has the maximum number of items")
	ErrInvalidChecklistOrder = errors.New("order must list every existing id exactly once")
)

type ChecklistItem struct {
	ID          uint         `json:"id"`
	ChecklistID uint         `json:"checklist_id"`
	Text        string       `json:"text"`
	Resolved    bool         `json:"resolved"`
	ResolvedAt  *time.Time   `json:"resolved_at"`
	ResolvedBy  *CommentUser `json:"resolved_by"`
	Assignee    *CommentUser `json:"assignee"`
	Position    int64        `json:"position"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

type Checklist struct {
	ID        uint            `json:"id"`
	TaskID    uint            `json:"task_id"`
	Name      string          `json:"name"`
	Position  int64           `json:"position"`
	Items     []ChecklistItem `json:"items"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

type ChecklistItemUpdate struct {
	Text     *string
	Resolved *bool
	Assignee Nullable[uint]
}

type ChecklistRepository interface {
	CreateChecklist(ctx context.Context, taskID uint, name string) (*Checklist, error)
	ListByTask(ctx context.Context, taskID uint) ([]Checklist, error)
	GetChecklist(ctx context.Context, taskID, checklistID uint) (*Checklist, error)
	RenameChecklist(ctx context.Context, taskID, checklistID uint, name string) (*Checklist, error)
	DeleteChecklist(ctx context.Context, taskID, checklistID uint) error
	ReorderChecklists(ctx context.Context, taskID uint, ids []uint) error

	CreateItem(ctx context.Context, taskID, checklistID uint, text string, assigneeID *uint) (*ChecklistItem, error)
	GetItem(ctx context.Context, taskID, checklistID, itemID uint) (*ChecklistItem, error)
	UpdateItem(ctx context.Context, taskID, checklistID, itemID, actorID uint, update ChecklistItemUpdate) (*ChecklistItem, error)
	DeleteItem(ctx context.Context, taskID, checklistID, itemID uint) error
	ReorderItems(ctx context.Context, taskID, checklistID uint, ids []uint) error
}
