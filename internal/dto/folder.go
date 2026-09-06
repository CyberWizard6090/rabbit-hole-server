package dto

type CreateFolderRequest struct {
	Name string `json:"name" binding:"required,min=2"`
}

type UpdateFolderRequest struct {
	Name *string `json:"name" binding:"omitempty,min=2"`
}
