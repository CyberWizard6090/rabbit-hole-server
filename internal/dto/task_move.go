package dto

type MoveTaskRequest struct {
	ListID   *uint `json:"list_id,omitempty"`
	StatusID *uint `json:"status_id,omitempty"`
	AfterID  *uint `json:"after_id,omitempty" binding:"omitempty,excluded_with=BeforeID"`
	BeforeID *uint `json:"before_id,omitempty"`
}
