package dto

type UpdateProfileRequest struct {
	Username  *string `json:"username" binding:"omitempty,min=3"`
	FirstName *string `json:"first_name"`
	LastName  *string `json:"last_name"`
	Bio       *string `json:"bio"`
	TimeZone  *string `json:"time_zone"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
