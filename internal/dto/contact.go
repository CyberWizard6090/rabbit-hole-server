package dto

type AddContactRequest struct {
	ContactID uint `json:"contact_id" binding:"required"`
}

type PublicUser struct {
	ID        uint   `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	AvatarURL string `json:"avatar_url"`
}
