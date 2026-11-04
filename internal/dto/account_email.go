package dto

import "strings"

type VerifyEmailRequest struct {
	Token string `json:"token" binding:"required,max=200"`
}

type EmailRequest struct {
	Email string `json:"email" binding:"required,email,max=254"`
}

type ResetPasswordRequest struct {
	Token       string `json:"token" binding:"required,max=200"`
	NewPassword string `json:"new_password" binding:"required,min=8,max=128"`
}

func (request *VerifyEmailRequest) UnmarshalJSON(data []byte) error {
	type plain VerifyEmailRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Token = strings.TrimSpace(value.Token)
	}); err != nil {
		return err
	}
	*request = VerifyEmailRequest(decoded)
	return nil
}

func (request *EmailRequest) UnmarshalJSON(data []byte) error {
	type plain EmailRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Email = strings.TrimSpace(value.Email)
	}); err != nil {
		return err
	}
	*request = EmailRequest(decoded)
	return nil
}

func (request *ResetPasswordRequest) UnmarshalJSON(data []byte) error {
	type plain ResetPasswordRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Token = strings.TrimSpace(value.Token)
	}); err != nil {
		return err
	}
	*request = ResetPasswordRequest(decoded)
	return nil
}
