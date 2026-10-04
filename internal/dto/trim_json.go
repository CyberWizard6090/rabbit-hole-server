package dto

import (
	"encoding/json"
	"strings"
)

func unmarshalJSONAndTrim[T any](data []byte, target *T, trim func(*T)) error {
	if err := json.Unmarshal(data, target); err != nil {
		return err
	}
	trim(target)
	return nil
}

func trimOptional(value *string) {
	if value != nil {
		*value = strings.TrimSpace(*value)
	}
}

func (request *CreateWorkspaceRequest) UnmarshalJSON(data []byte) error {
	type plain CreateWorkspaceRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Name = strings.TrimSpace(value.Name)
	}); err != nil {
		return err
	}
	*request = CreateWorkspaceRequest(decoded)
	return nil
}

func (request *UpdateWorkspaceRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateWorkspaceRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		trimOptional(value.Name)
	}); err != nil {
		return err
	}
	*request = UpdateWorkspaceRequest(decoded)
	return nil
}

func (request *CreateSpaceRequest) UnmarshalJSON(data []byte) error {
	type plain CreateSpaceRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Name = strings.TrimSpace(value.Name)
	}); err != nil {
		return err
	}
	*request = CreateSpaceRequest(decoded)
	return nil
}

func (request *CreateListRequest) UnmarshalJSON(data []byte) error {
	type plain CreateListRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Name = strings.TrimSpace(value.Name)
	}); err != nil {
		return err
	}
	*request = CreateListRequest(decoded)
	return nil
}

func (request *CreateFolderRequest) UnmarshalJSON(data []byte) error {
	type plain CreateFolderRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Name = strings.TrimSpace(value.Name)
	}); err != nil {
		return err
	}
	*request = CreateFolderRequest(decoded)
	return nil
}

func (request *UpdateFolderRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateFolderRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		trimOptional(value.Name)
	}); err != nil {
		return err
	}
	*request = UpdateFolderRequest(decoded)
	return nil
}

func (request *CreateStatusRequest) UnmarshalJSON(data []byte) error {
	type plain CreateStatusRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Name = strings.TrimSpace(value.Name)
	}); err != nil {
		return err
	}
	*request = CreateStatusRequest(decoded)
	return nil
}

func (request *UpdateStatusRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateStatusRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		trimOptional(value.Name)
	}); err != nil {
		return err
	}
	*request = UpdateStatusRequest(decoded)
	return nil
}

func (request *CreateTaskRequest) UnmarshalJSON(data []byte) error {
	type plain CreateTaskRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		value.Title = strings.TrimSpace(value.Title)
		for i := range value.NewTagNames {
			value.NewTagNames[i] = strings.TrimSpace(value.NewTagNames[i])
		}
	}); err != nil {
		return err
	}
	*request = CreateTaskRequest(decoded)
	return nil
}

func (request *UpdateTaskRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateTaskRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		trimOptional(value.Title)
		for i := range value.AddTagNames {
			value.AddTagNames[i] = strings.TrimSpace(value.AddTagNames[i])
		}
	}); err != nil {
		return err
	}
	*request = UpdateTaskRequest(decoded)
	return nil
}

func (request *UpdateProfileRequest) UnmarshalJSON(data []byte) error {
	type plain UpdateProfileRequest
	var decoded plain
	if err := unmarshalJSONAndTrim(data, &decoded, func(value *plain) {
		trimOptional(value.Username)
	}); err != nil {
		return err
	}
	*request = UpdateProfileRequest(decoded)
	return nil
}
