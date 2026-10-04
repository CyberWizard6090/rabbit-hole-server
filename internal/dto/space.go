package dto

type CreateSpaceRequest struct {
	Name     string `json:"name" binding:"required,min=1"`
	Currency string `json:"currency"`
}

type CreateListRequest struct {
	Name           string `json:"name" binding:"required,min=1"`
	ParentStatusID *uint  `json:"parent_status_id"`
}
