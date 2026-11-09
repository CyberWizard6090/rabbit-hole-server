package domain

import (
	"context"
	"errors"
)

const (
	RelationBlocks    = "blocks"
	RelationBlockedBy = "blocked_by"
	RelationRelated   = "related"

	MaxRelationsPerTask = 50
)

var (
	ErrRelationSelf      = errors.New("a task cannot be related to itself")
	ErrRelationExists    = errors.New("these tasks are already related")
	ErrRelationCycle     = errors.New("this would make the tasks block each other in a circle")
	ErrRelationLimit     = errors.New("too many relations on a task")
	ErrRelationMissing   = errors.New("relation not found")
	ErrRelationBadKind   = errors.New("unknown relation kind")
	ErrRelationOtherTask = errors.New("the other task was not found")
)

type RelatedTask struct {
	ID       uint   `json:"id"`
	Title    string `json:"title"`
	ListID   uint   `json:"list_id"`
	StatusID uint   `json:"status_id"`
	Done     bool   `json:"done"`
}

type TaskRelations struct {
	Blocks    []RelatedTask `json:"blocks"`
	BlockedBy []RelatedTask `json:"blocked_by"`
	Related   []RelatedTask `json:"related"`
	Blocked   bool          `json:"blocked"`
}

type TaskRelationRepository interface {
	Add(ctx context.Context, taskID, otherID uint, kind string, userID uint) error
	Remove(ctx context.Context, taskID, otherID uint, kind string) error
	List(ctx context.Context, taskID uint) (*TaskRelations, error)
}
