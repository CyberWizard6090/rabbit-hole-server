package dto

import "strings"

type ChangeEmailRequest struct {
	NewEmail string `json:"new_email" binding:"required,email,max=254"`
	Password string `json:"password" binding:"required"`
}

type ConfirmEmailChangeRequest struct {
	Token string `json:"token" binding:"required,max=200"`
}

func (request *ChangeEmailRequest) UnmarshalJSON(data []byte) error {
	type plain ChangeEmailRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.NewEmail = strings.TrimSpace(value.NewEmail)
	}); err != nil {
		return err
	}
	*request = ChangeEmailRequest(decoded)
	return nil
}

func (request *ConfirmEmailChangeRequest) UnmarshalJSON(data []byte) error {
	type plain ConfirmEmailChangeRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Token = strings.TrimSpace(value.Token)
	}); err != nil {
		return err
	}
	*request = ConfirmEmailChangeRequest(decoded)
	return nil
}
