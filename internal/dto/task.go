package dto

type CreateTaskRequest struct {
	Title        string   `json:"title" binding:"required,min=1"`
	Description  string   `json:"description"`
	StatusID     uint     `json:"status_id" binding:"required"`
	ParentID     *uint    `json:"parent_id,omitempty"`
	Priority     int      `json:"priority" binding:"gte=0,lte=5"`
	AssigneeIDs  []uint   `json:"assignee_ids"`
	TagIDs       []uint   `json:"tag_ids"`
	NewTagNames  []string `json:"new_tag_names"`
	TimeEstimate int      `json:"time_estimate" binding:"gte=0"`
}

type UpdateTaskRequest struct {
	Title        *string  `json:"title,omitempty" binding:"omitempty,min=1"`
	Description  *string  `json:"description,omitempty"`
	StatusID     *uint    `json:"status_id,omitempty"`
	Priority     *int     `json:"priority,omitempty" binding:"omitempty,gte=1,lte=5"`
	TimeEstimate *int     `json:"time_estimate,omitempty" binding:"omitempty,gte=0"`
	AddTagIDs    []uint   `json:"add_tag_ids,omitempty"`
	AddTagNames  []string `json:"add_tag_names,omitempty"`
}
