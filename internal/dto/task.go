package dto

import (
	"encoding/json"
	"time"

	"rabbit-hole-server/internal/domain"
)

// RecurrenceRequest is the repeat rule a client sends. The server owns the position in the series.
type RecurrenceRequest struct {
	Frequency string `json:"frequency"`
	Interval  int    `json:"interval"`
	Weekdays  []int  `json:"weekdays"`
	MonthDay  int    `json:"month_day"`
	Anchor    string `json:"anchor"`
	TimeZone  string `json:"time_zone"`
	Until     string `json:"until"`
	Count     int    `json:"count"`
}

func (r RecurrenceRequest) Domain() domain.Recurrence {
	return domain.Recurrence{
		Frequency: domain.RecurrenceFrequency(r.Frequency), Interval: r.Interval, Weekdays: r.Weekdays,
		MonthDay: r.MonthDay, Anchor: domain.RecurrenceAnchor(r.Anchor), TimeZone: r.TimeZone, Until: r.Until, Count: r.Count,
	}
}

func recurrenceToDomain(r *RecurrenceRequest) *domain.Recurrence {
	if r == nil {
		return nil
	}
	rule := r.Domain()
	return &rule
}

// RecurrenceOrNil converts an optional rule of a create request.
func (r CreateTaskRequest) RecurrenceOrNil() *domain.Recurrence {
	return recurrenceToDomain(r.Recurrence)
}

// RecurrenceChange converts the rule of an update request: not sent leaves it, null removes it.
func (r UpdateTaskRequest) RecurrenceChange() domain.Nullable[domain.Recurrence] {
	if !r.Recurrence.Set {
		return domain.Nullable[domain.Recurrence]{}
	}
	return domain.Nullable[domain.Recurrence]{Set: true, Value: recurrenceToDomain(r.Recurrence.Value)}
}

type CreateTaskRequest struct {
	Title        string     `json:"title" binding:"required,min=1,max=500"`
	Description  string     `json:"description" binding:"max=50000"`
	StatusID     uint       `json:"status_id" binding:"required"`
	ParentID     *uint      `json:"parent_id,omitempty"`
	Priority     int        `json:"priority" binding:"gte=0,lte=5"`
	AssigneeIDs  []uint     `json:"assignee_ids" binding:"max=100"`
	TagIDs       []uint     `json:"tag_ids" binding:"max=100"`
	NewTagNames  []string   `json:"new_tag_names" binding:"max=50,dive,max=50"`
	StartDate    *time.Time `json:"start_date,omitempty"`
	DueDate      *time.Time `json:"due_date,omitempty"`
	TimeEstimate int        `json:"time_estimate" binding:"gte=0,lte=2147483647"`

	CustomFields map[uint]json.RawMessage `json:"custom_fields"`
	Recurrence   *RecurrenceRequest       `json:"recurrence"`
}

type UpdateTaskRequest struct {
	Title        *string                            `json:"title,omitempty" binding:"omitempty,min=1,max=500"`
	Description  *string                            `json:"description,omitempty" binding:"omitempty,max=50000"`
	StatusID     *uint                              `json:"status_id,omitempty"`
	ParentID     domain.Nullable[uint]              `json:"parent_id"`
	Priority     *int                               `json:"priority,omitempty" binding:"omitempty,gte=1,lte=5"`
	StartDate    domain.Nullable[time.Time]         `json:"start_date"`
	DueDate      domain.Nullable[time.Time]         `json:"due_date"`
	TimeEstimate *int                               `json:"time_estimate,omitempty" binding:"omitempty,gte=0,lte=2147483647"`
	AssigneeIDs  *[]uint                            `json:"assignee_ids,omitempty" binding:"omitempty,max=100"`
	AddTagIDs    []uint                             `json:"add_tag_ids,omitempty" binding:"max=100"`
	AddTagNames  []string                           `json:"add_tag_names,omitempty" binding:"max=50,dive,max=50"`
	RemoveTagIDs []uint                             `json:"remove_tag_ids,omitempty" binding:"max=100,dive,gt=0"`
	Recurrence   domain.Nullable[RecurrenceRequest] `json:"recurrence"`
}

const MaxBulkTasks = 100

type BulkUpdateFields struct {
	StatusID     *uint                      `json:"status_id,omitempty"`
	Priority     *int                       `json:"priority,omitempty" binding:"omitempty,gte=1,lte=5"`
	DueDate      domain.Nullable[time.Time] `json:"due_date"`
	AssigneeIDs  *[]uint                    `json:"assignee_ids,omitempty" binding:"omitempty,max=100"`
	AddTagIDs    []uint                     `json:"add_tag_ids,omitempty" binding:"max=100,dive,gt=0"`
	RemoveTagIDs []uint                     `json:"remove_tag_ids,omitempty" binding:"max=100,dive,gt=0"`
}

func (f BulkUpdateFields) Empty() bool {
	return f.StatusID == nil && f.Priority == nil && !f.DueDate.Set && f.AssigneeIDs == nil &&
		len(f.AddTagIDs) == 0 && len(f.RemoveTagIDs) == 0
}

type BulkMoveFields struct {
	ListID   uint  `json:"list_id" binding:"required,gt=0"`
	StatusID *uint `json:"status_id,omitempty"`
}

type BulkTasksRequest struct {
	TaskIDs []uint            `json:"task_ids" binding:"required,min=1,max=100,dive,gt=0"`
	Action  string            `json:"action" binding:"required,oneof=update move delete"`
	Update  *BulkUpdateFields `json:"update"`
	Move    *BulkMoveFields   `json:"move"`
}

type BulkTaskResult struct {
	TaskID uint   `json:"task_id"`
	OK     bool   `json:"ok"`
	Code   string `json:"code,omitempty"`
}

type BulkTasksResponse struct {
	Results   []BulkTaskResult `json:"results"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
}

type StartTimerRequest struct {
	Note string `json:"note" binding:"max=2000"`
}

type AddTimeEntryRequest struct {
	Seconds   int        `json:"seconds" binding:"required,gte=1,lte=86400"`
	StartedAt *time.Time `json:"started_at"`
	Note      string     `json:"note" binding:"max=2000"`
}

type UpdateTimeEntryRequest struct {
	Seconds *int    `json:"seconds" binding:"omitempty,gte=1,lte=86400"`
	Note    *string `json:"note" binding:"omitempty,max=2000"`
}

type AddRelationRequest struct {
	TaskID uint   `json:"task_id" binding:"required,gt=0"`
	Kind   string `json:"kind" binding:"required,oneof=blocks blocked_by related"`
}
