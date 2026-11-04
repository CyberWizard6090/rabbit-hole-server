package domain

import (
	"context"
	"errors"
	"time"

	"rabbit-hole-server/internal/http/pagination"
)

var (
	ErrInvalidStatusReference   = errors.New("status does not belong to task list")
	ErrInvalidParentReference   = errors.New("parent task does not belong to task list")
	ErrInvalidTagReference      = errors.New("tag does not belong to task space")
	ErrInvalidAssigneeReference = errors.New("assignee does not belong to task workspace")
)

type Task struct {
	Model
	SpaceID      uint       `json:"-"`
	ListID       uint       `json:"list_id"`
	StatusID     uint       `json:"status_id"`
	ParentID     *uint      `json:"parent_id"`
	Title        string     `json:"title" binding:"required,min=3"`
	Description  string     `json:"description"`
	Priority     int        `json:"priority"`
	UserID       uint       `json:"user_id"`
	Assignees    []User     `json:"assignees"`
	Tags         []Tag      `json:"tags"`
	StartDate    *time.Time `json:"start_date"`
	DueDate      *time.Time `json:"due_date"`
	TimeEstimate int        `json:"time_estimate"`
	TimeSpent    int        `json:"time_spent"`

	SortStatusPosition int     `json:"-"`
	SortAssignee       *string `json:"-"`
}

type TaskRepository interface {
	Create(ctx context.Context, task *Task, assigneeIDs []uint, tagIDs []uint) error
	List(ctx context.Context, listID uint, query TaskQuery, params pagination.Params) ([]Task, bool, bool, error)
	Count(ctx context.Context, listID uint, filter TaskFilter) (int64, error)
	GetByID(ctx context.Context, id uint) (*Task, error)
	Update(ctx context.Context, task *Task, addTagIDs []uint) error
	Delete(ctx context.Context, id uint) error
}
