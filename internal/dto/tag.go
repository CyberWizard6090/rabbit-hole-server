package dto

type CreateTagRequest struct {
	Name  string `json:"name" binding:"required"`
	Color string `json:"color" binding:"required"`
}

type UpdateTagRequest struct {
	Name  *string `json:"name,omitempty"`
	Color *string `json:"color,omitempty"`
}

type MergeTagsRequest struct {
	SourceTagID uint `json:"source_tag_id" binding:"required"`
	TargetTagID uint `json:"target_tag_id" binding:"required"`
}
