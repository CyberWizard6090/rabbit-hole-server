package dto

type CreateTagRequest struct {
	Name  string `json:"name" binding:"required,min=1,max=50"`
	Color string `json:"color" binding:"required,hexcolor,len=7"`
}

type UpdateTagRequest struct {
	Name  *string `json:"name,omitempty" binding:"omitempty,min=1,max=50"`
	Color *string `json:"color,omitempty" binding:"omitempty,hexcolor,len=7"`
}

type MergeTagsRequest struct {
	SourceTagID uint `json:"source_tag_id" binding:"required"`
	TargetTagID uint `json:"target_tag_id" binding:"required"`
}
