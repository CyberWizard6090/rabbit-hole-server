package dto

import "strings"

type AddChannelRequest struct {
	Kind        string `json:"kind" binding:"required,max=32"`
	Destination string `json:"destination" binding:"required,max=254"`
}

type ConfirmChannelRequest struct {
	Token string `json:"token" binding:"required,max=200"`
}

type PreferenceInput struct {
	Type    string `json:"type" binding:"required,max=64"`
	Channel string `json:"channel" binding:"required,max=32"`
	Enabled *bool  `json:"enabled" binding:"required"`
}

type SavePreferencesRequest struct {
	Preferences []PreferenceInput `json:"preferences" binding:"required,min=1,max=100,dive"`
}

type MarkReadRequest struct {
	IDs  []uint `json:"ids" binding:"omitempty,max=200"`
	All  bool   `json:"all"`
	Type string `json:"type" binding:"omitempty,max=64"`
}

type NotificationSettingsRequest struct {
	Locale string `json:"locale" binding:"required,max=8"`
}

func (request *AddChannelRequest) UnmarshalJSON(data []byte) error {
	type plain AddChannelRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Kind = strings.TrimSpace(value.Kind)
		value.Destination = strings.TrimSpace(value.Destination)
	}); err != nil {
		return err
	}
	*request = AddChannelRequest(decoded)
	return nil
}

func (request *ConfirmChannelRequest) UnmarshalJSON(data []byte) error {
	type plain ConfirmChannelRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Token = strings.TrimSpace(value.Token)
	}); err != nil {
		return err
	}
	*request = ConfirmChannelRequest(decoded)
	return nil
}
