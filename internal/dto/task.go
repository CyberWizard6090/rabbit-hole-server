package dto

import (
	"encoding/json"
	"time"

	"rabbit-hole-server/internal/domain"
)

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
}

type UpdateTaskRequest struct {
	Title        *string                    `json:"title,omitempty" binding:"omitempty,min=1,max=500"`
	Description  *string                    `json:"description,omitempty" binding:"omitempty,max=50000"`
	StatusID     *uint                      `json:"status_id,omitempty"`
	ParentID     domain.Nullable[uint]      `json:"parent_id"`
	Priority     *int                       `json:"priority,omitempty" binding:"omitempty,gte=1,lte=5"`
	StartDate    domain.Nullable[time.Time] `json:"start_date"`
	DueDate      domain.Nullable[time.Time] `json:"due_date"`
	TimeEstimate *int                       `json:"time_estimate,omitempty" binding:"omitempty,gte=0,lte=2147483647"`
	AssigneeIDs  *[]uint                    `json:"assignee_ids,omitempty" binding:"omitempty,max=100"`
	AddTagIDs    []uint                     `json:"add_tag_ids,omitempty" binding:"max=100"`
	AddTagNames  []string                   `json:"add_tag_names,omitempty" binding:"max=50,dive,max=50"`
	RemoveTagIDs []uint                     `json:"remove_tag_ids,omitempty" binding:"max=100,dive,gt=0"`
}
