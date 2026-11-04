package domain

import (
	"context"
	"encoding/json"
	"time"
)

const (
	ActivityTaskCreated    = "task.created"
	ActivityTaskUpdated    = "task.updated"
	ActivityTaskMoved      = "task.moved"
	ActivityCommentCreated = "comment.created"
	ActivityCommentUpdated = "comment.updated"
	ActivityCommentDeleted = "comment.deleted"

	ActivityChecklistCreated      = "checklist.created"
	ActivityChecklistDeleted      = "checklist.deleted"
	ActivityChecklistItemResolved = "checklist.item_resolved"
	ActivityChecklistItemReopened = "checklist.item_reopened"
	ActivityChecklistItemAssigned = "checklist.item_assigned"

	ActivityAttachmentCreated = "attachment.created"
	ActivityAttachmentDeleted = "attachment.deleted"
)

type Activity struct {
	ID        uint            `json:"id"`
	TaskID    uint            `json:"task_id"`
	Actor     CommentUser     `json:"actor"`
	Action    string          `json:"action"`
	Field     string          `json:"field,omitempty"`
	OldValue  json.RawMessage `json:"old_value,omitempty"`
	NewValue  json.RawMessage `json:"new_value,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type ActivityEntry struct {
	TaskID   uint
	ActorID  uint
	Action   string
	Field    string
	OldValue json.RawMessage
	NewValue json.RawMessage
}

type ActivityRepository interface {
	Record(ctx context.Context, entries []ActivityEntry) error
	ListByTask(ctx context.Context, taskID uint, limit, offset int) ([]Activity, int64, error)
}
