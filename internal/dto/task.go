package dto

type CreateTaskRequest struct {
	Title        string   `json:"title" binding:"required,min=3"`
	Description  string   `json:"description"`
	StatusID     uint     `json:"status_id" binding:"required"`
	ParentID     *uint    `json:"parent_id,omitempty"`
	Priority     int      `json:"priority"`
	AssigneeIDs  []uint   `json:"assignee_ids"`
	TagIDs       []uint   `json:"tag_ids"`
	NewTagNames  []string `json:"new_tag_names"`
	TimeEstimate int      `json:"time_estimate"`
}

type UpdateTaskRequest struct {
	Title        *string  `json:"title,omitempty"`
	Description  *string  `json:"description,omitempty"`
	StatusID     *uint    `json:"status_id,omitempty"`
	Priority     *int     `json:"priority,omitempty"`
	TimeEstimate *int     `json:"time_estimate,omitempty"`
	AddTagIDs    []uint   `json:"add_tag_ids,omitempty"`
	AddTagNames  []string `json:"add_tag_names,omitempty"`
}
