package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"rabbit-hole-server/internal/http/pagination"
)

var (
	ErrInvalidStatusReference   = errors.New("status does not belong to task list")
	ErrInvalidParentReference   = errors.New("parent task does not belong to task list")
	ErrInvalidTagReference      = errors.New("tag does not belong to task space")
	ErrInvalidAssigneeReference = errors.New("assignee does not belong to task workspace")
	ErrInvalidDateRange         = errors.New("due date must not be before start date")
	ErrInvalidListReference     = errors.New("target list does not belong to the task workspace")
	ErrInvalidTaskSource        = errors.New("task source metadata is invalid")
	ErrInvalidMoveAnchor        = errors.New("anchor task does not belong to the target list")
	ErrConcurrentMove           = errors.New("task was changed by another request, retry the move")
	ErrTaskHasSubtasks          = errors.New("task with subtasks cannot be moved to another list")
	ErrConflictingTagChange     = errors.New("a tag cannot be added and removed in the same request")
)

type Task struct {
	Model
	SpaceID           uint          `json:"-"`
	ListID            uint          `json:"list_id"`
	StatusID          uint          `json:"status_id"`
	ParentID          *uint         `json:"parent_id"`
	Title             string        `json:"title" binding:"required,min=3"`
	Description       string        `json:"description"`
	Priority          int           `json:"priority"`
	UserID            uint          `json:"user_id"`
	Assignees         []CommentUser `json:"assignees"`
	Tags              []Tag         `json:"tags"`
	StartDate         *time.Time    `json:"start_date"`
	DueDate           *time.Time    `json:"due_date"`
	TimeEstimate      int           `json:"time_estimate"`
	TimeSpent         int           `json:"time_spent"`
	Position          int64         `json:"position"`
	Source            TaskSource    `json:"source"`
	SourceReferenceID *uint         `json:"source_reference_id,omitempty"`
	Actor             Actor         `json:"-"`
	CreatedBy         ActorView     `json:"created_by"`
	RequesterInfo     *Requester    `json:"-"`
	Requester         *Requester    `json:"requester,omitempty"`

	SubtaskCount int `json:"subtask_count"`

	ChecklistItemsTotal    int `json:"checklist_items_total"`
	ChecklistItemsResolved int `json:"checklist_items_resolved"`

	SortStatusPosition int     `json:"-"`
	SortAssignee       *string `json:"-"`
}

// MarshalJSON never sends null for assignees or tags: clients can iterate them without a check,
// whichever endpoint built the task.
func (t Task) MarshalJSON() ([]byte, error) {
	type plain Task
	if t.Assignees == nil {
		t.Assignees = []CommentUser{}
	}
	if t.Tags == nil {
		t.Tags = []Tag{}
	}
	return json.Marshal(plain(t))
}

func (t Task) RevealRequester() Task {
	t.Requester = t.RequesterInfo
	return t
}

type TaskSource string

const (
	TaskSourceManual     TaskSource = "manual"
	TaskSourceForm       TaskSource = "form"
	TaskSourceAPI        TaskSource = "api"
	TaskSourceAutomation TaskSource = "automation"
)

type TaskRepository interface {
	Create(ctx context.Context, task *Task, assigneeIDs []uint, tagIDs []uint) error
	List(ctx context.Context, listID uint, query TaskQuery, params pagination.Params) ([]Task, bool, bool, error)
	Count(ctx context.Context, listID uint, filter TaskFilter) (int64, error)
	GetByID(ctx context.Context, id uint) (*Task, error)
	Update(ctx context.Context, id uint, apply func(*Task) error, addTagIDs, removeTagIDs []uint, replaceAssigneeIDs *[]uint) (before, after *Task, err error)
	Delete(ctx context.Context, id uint) error
	Move(ctx context.Context, id uint, target TaskMove) (*Task, error)
}

type TaskMove struct {
	ListID   uint
	StatusID uint
	AfterID  *uint
	BeforeID *uint
}
