package dto

import (
	"encoding/json"
	"time"

	"rabbit-hole-server/internal/domain"
)

type CreateTaskRequest struct {
	Title        string     `json:"title" binding:"required,min=1"`
	Description  string     `json:"description"`
	StatusID     uint       `json:"status_id" binding:"required"`
	ParentID     *uint      `json:"parent_id,omitempty"`
	Priority     int        `json:"priority" binding:"gte=0,lte=5"`
	AssigneeIDs  []uint     `json:"assignee_ids"`
	TagIDs       []uint     `json:"tag_ids"`
	NewTagNames  []string   `json:"new_tag_names"`
	StartDate    *time.Time `json:"start_date,omitempty"`
	DueDate      *time.Time `json:"due_date,omitempty"`
	TimeEstimate int        `json:"time_estimate" binding:"gte=0"`

	CustomFields map[uint]json.RawMessage `json:"custom_fields"`
}

type UpdateTaskRequest struct {
	Title        *string                    `json:"title,omitempty" binding:"omitempty,min=1"`
	Description  *string                    `json:"description,omitempty"`
	StatusID     *uint                      `json:"status_id,omitempty"`
	ParentID     domain.Nullable[uint]      `json:"parent_id"`
	Priority     *int                       `json:"priority,omitempty" binding:"omitempty,gte=1,lte=5"`
	StartDate    domain.Nullable[time.Time] `json:"start_date"`
	DueDate      domain.Nullable[time.Time] `json:"due_date"`
	TimeEstimate *int                       `json:"time_estimate,omitempty" binding:"omitempty,gte=0"`
	AssigneeIDs  *[]uint                    `json:"assignee_ids,omitempty"`
	AddTagIDs    []uint                     `json:"add_tag_ids,omitempty"`
	AddTagNames  []string                   `json:"add_tag_names,omitempty"`
	RemoveTagIDs []uint                     `json:"remove_tag_ids,omitempty" binding:"dive,gt=0"`
}
