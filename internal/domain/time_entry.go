package domain

import (
	"context"
	"errors"
	"time"
)

const (
	MaxTimeEntrySeconds = 86400
	MaxTimeNoteRunes    = 500
)

var (
	ErrTimerRunning     = errors.New("you already have a running timer")
	ErrNoRunningTimer   = errors.New("no running timer on this task")
	ErrTimeEntryMissing = errors.New("time entry not found")
	ErrInvalidTimeEntry = errors.New("invalid time entry")
	ErrTimeForbidden    = errors.New("you cannot change this time entry")
)

type TimeEntry struct {
	ID        uint         `json:"id"`
	TaskID    uint         `json:"task_id"`
	UserID    uint         `json:"-"`
	User      *CommentUser `json:"user"`
	StartedAt time.Time    `json:"started_at"`
	EndedAt   *time.Time   `json:"ended_at"`
	Seconds   int          `json:"seconds"`
	Running   bool         `json:"running"`
	Note      string       `json:"note"`
	CreatedAt time.Time    `json:"created_at"`
}

type RunningTimer struct {
	EntryID   uint      `json:"entry_id"`
	TaskID    uint      `json:"task_id"`
	TaskTitle string    `json:"task_title"`
	StartedAt time.Time `json:"started_at"`
	Seconds   int       `json:"seconds"`
}

type TimeReportRow struct {
	User    *CommentUser `json:"user"`
	Seconds int64        `json:"seconds"`
	Entries int64        `json:"entries"`
}

type TimeEntryDraft struct {
	TaskID    uint
	UserID    uint
	StartedAt time.Time
	EndedAt   time.Time
	Seconds   int
	Note      string
}

type TimeEntryRepository interface {
	Start(ctx context.Context, taskID, userID uint, note string) (*TimeEntry, error)
	Stop(ctx context.Context, taskID, userID uint) (*TimeEntry, error)
	Add(ctx context.Context, draft TimeEntryDraft) (*TimeEntry, error)
	List(ctx context.Context, taskID uint) ([]TimeEntry, error)
	Get(ctx context.Context, taskID, entryID uint) (*TimeEntry, error)
	Update(ctx context.Context, taskID, entryID uint, seconds *int, note *string) (*TimeEntry, error)
	Delete(ctx context.Context, taskID, entryID uint) error
	Running(ctx context.Context, userID uint) (*RunningTimer, error)
	Report(ctx context.Context, workspaceID uint, from, to time.Time, userID *uint) ([]TimeReportRow, error)
}
