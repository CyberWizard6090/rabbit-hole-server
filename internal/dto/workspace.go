package dto

type CreateWorkspaceRequest struct {
	Name        string `json:"name" binding:"required,min=1,max=255"`
	Description string `json:"description" binding:"max=5000"`
}

type UpdateWorkspaceRequest struct {
	Name        *string `json:"name,omitempty" binding:"omitempty,min=1,max=255"`
	Description *string `json:"description,omitempty" binding:"omitempty,max=5000"`
}

type AddMemberRequest struct {
	UserID uint   `json:"user_id" binding:"required"`
	Role   string `json:"role" binding:"required,oneof=Admin Member Guest"`
}

type UpdateMemberRoleRequest struct {
	Role string `json:"role" binding:"required,oneof=Admin Member Guest"`
}

type MemberResponse struct {
	UserID    uint   `json:"user_id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	AvatarURL string `json:"avatar_url"`
	Role      string `json:"role"`
}
