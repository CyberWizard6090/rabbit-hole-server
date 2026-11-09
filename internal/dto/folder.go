package dto

type CreateFolderRequest struct {
	Name string `json:"name" binding:"required,min=1,max=255"`
}

type UpdateFolderRequest struct {
	Name *string `json:"name" binding:"omitempty,min=1,max=255"`
}
