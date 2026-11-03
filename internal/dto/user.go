package dto

type UpdateProfileRequest struct {
	// omitnil (not omitempty): an absent field is skipped, but a present empty
	// string is validated, so "" can neither blank the username nor the time zone.
	Username  *string `json:"username" binding:"omitnil,min=3,max=30"`
	FirstName *string `json:"first_name" binding:"omitnil,max=100"`
	LastName  *string `json:"last_name" binding:"omitnil,max=100"`
	Bio       *string `json:"bio" binding:"omitnil,max=2000"`
	TimeZone  *string `json:"time_zone" binding:"omitnil,timezone"`
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}
