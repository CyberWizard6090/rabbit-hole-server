package dto

import "strings"

type CreateCommentRequest struct {
	Body       string `json:"body" binding:"required,max=20000"`
	MentionIDs []uint `json:"mention_user_ids" binding:"omitempty,max=50"`
}

type UpdateCommentRequest struct {
	Body       *string `json:"body,omitempty" binding:"omitempty,min=1,max=20000"`
	MentionIDs *[]uint `json:"mention_user_ids,omitempty" binding:"omitempty,max=50"`
}

func (request *CreateCommentRequest) UnmarshalJSON(data []byte) error {
	type plain CreateCommentRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Body = strings.TrimSpace(value.Body)
	}); err != nil {
		return err
	}
	*request = CreateCommentRequest(decoded)
	return nil
}

func (request *UpdateCommentRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateCommentRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		trimOptional(value.Body)
	}); err != nil {
		return err
	}
	*request = UpdateCommentRequest(decoded)
	return nil
}
