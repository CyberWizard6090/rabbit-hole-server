package dto

type CreateWorkspaceRequest struct {
	Name        string `json:"name" binding:"required,min=2,max=255"`
	Description string `json:"description"`
}

type UpdateWorkspaceRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=2,max=255"`
	Description *string `json:"description,omitempty"`
}
