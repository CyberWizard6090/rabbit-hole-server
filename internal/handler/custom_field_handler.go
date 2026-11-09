package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type CustomFieldHandler struct {
	service service.CustomFieldService
}

func NewCustomFieldHandler(service service.CustomFieldService) *CustomFieldHandler {
	return &CustomFieldHandler{service: service}
}

func (h *CustomFieldHandler) Create(c *gin.Context) {
	workspaceID, ok := parseCustomFieldID(c, "workspace_id")
	if !ok {
		return
	}
	var req dto.CustomFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	field, err := h.service.Create(c.Request.Context(), workspaceID, customFieldInput(req))
	if err != nil {
		h.writeError(c, "CUSTOM_FIELD_CREATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, field)
}

func (h *CustomFieldHandler) List(c *gin.Context) {
	workspaceID, ok := parseCustomFieldID(c, "workspace_id")
	if !ok {
		return
	}
	fields, err := h.service.List(c.Request.Context(), workspaceID)
	if err != nil {
		response.HandleError(c, httperrors.Internal("CUSTOM_FIELDS_FETCH_FAILED", "internal server error", err))
		return
	}
	response.Success(c, http.StatusOK, fields)
}

func (h *CustomFieldHandler) Get(c *gin.Context) {
	workspaceID, fieldID, ok := parseCustomFieldRouteIDs(c)
	if !ok {
		return
	}
	field, err := h.service.Get(c.Request.Context(), workspaceID, fieldID)
	if err != nil {
		h.writeError(c, "CUSTOM_FIELD_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, field)
}

func (h *CustomFieldHandler) Update(c *gin.Context) {
	workspaceID, fieldID, ok := parseCustomFieldRouteIDs(c)
	if !ok {
		return
	}
	var req dto.CustomFieldRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	field, err := h.service.Update(c.Request.Context(), workspaceID, fieldID, customFieldInput(req))
	if err != nil {
		h.writeError(c, "CUSTOM_FIELD_UPDATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, field)
}

func (h *CustomFieldHandler) Delete(c *gin.Context) {
	workspaceID, fieldID, ok := parseCustomFieldRouteIDs(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), workspaceID, fieldID); err != nil {
		h.writeError(c, "CUSTOM_FIELD_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "custom field deleted"})
}

func (h *CustomFieldHandler) GetTaskValues(c *gin.Context) {
	taskID, ok := parseCustomFieldID(c, "task_id")
	if !ok {
		return
	}
	values, err := h.service.GetTaskValues(c.Request.Context(), taskID)
	if err != nil {
		h.writeError(c, "TASK_CUSTOM_FIELDS_FETCH_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, values)
}

func (h *CustomFieldHandler) SetTaskValue(c *gin.Context) {
	taskID, fieldID, ok := parseTaskCustomFieldRouteIDs(c)
	if !ok {
		return
	}
	var req dto.CustomFieldValueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if len(req.Value) == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_CUSTOM_FIELD", "value is required", nil).
			WithDetails(httperrors.Detail{Field: "value", Code: "REQUIRED"}))
		return
	}
	value, err := decodeCustomFieldValue(req.Value)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_CUSTOM_FIELD", "invalid value", err).
			WithDetails(httperrors.Detail{Field: "value", Code: "INVALID_VALUE"}))
		return
	}
	if err := h.service.SetTaskValue(c.Request.Context(), taskID, fieldID, value); err != nil {
		h.writeError(c, "TASK_CUSTOM_FIELD_UPDATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "custom field value saved"})
}

func (h *CustomFieldHandler) DeleteTaskValue(c *gin.Context) {
	taskID, fieldID, ok := parseTaskCustomFieldRouteIDs(c)
	if !ok {
		return
	}
	if err := h.service.DeleteTaskValue(c.Request.Context(), taskID, fieldID); err != nil {
		h.writeError(c, "TASK_CUSTOM_FIELD_DELETE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "custom field value deleted"})
}

func decodeCustomFieldValue(raw json.RawMessage) (any, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func decodeCustomFieldValues(raw map[uint]json.RawMessage) (map[uint]any, error) {
	values := make(map[uint]any, len(raw))
	for fieldID, item := range raw {
		value, err := decodeCustomFieldValue(item)
		if err != nil {
			return nil, err
		}
		values[fieldID] = value
	}
	return values, nil
}

func requiredCustomFieldError(err error) (*httperrors.AppError, bool) {
	var required *domain.CustomFieldRequiredError
	if !errors.As(err, &required) {
		return nil, false
	}
	details := make([]httperrors.Detail, len(required.FieldIDs))
	for i, id := range required.FieldIDs {
		details[i] = httperrors.Detail{
			Field:  "custom_fields." + strconv.FormatUint(uint64(id), 10),
			Code:   "REQUIRED",
			Params: map[string]any{"field_id": id},
		}
	}
	return httperrors.ValidationFailed(details, err), true
}

func customFieldEditError(err error) (*httperrors.AppError, bool) {
	var edit *domain.CustomFieldEditError
	if !errors.As(err, &edit) {
		return nil, false
	}
	code := "CUSTOM_FIELD_IN_USE"
	if errors.Is(edit.Cause, domain.ErrCustomFieldValueOutOfRange) {
		code = "CUSTOM_FIELD_VALUE_OUT_OF_RANGE"
	}
	appErr := httperrors.Conflict(code, err.Error(), err)
	appErr.Details = []httperrors.Detail{{Field: edit.Field, Code: edit.Code, Params: edit.Params}}
	return appErr, true
}

func (h *CustomFieldHandler) writeError(c *gin.Context, code string, err error) {
	if appErr, ok := requiredCustomFieldError(err); ok {
		response.HandleError(c, appErr)
		return
	}
	if appErr, ok := customFieldEditError(err); ok {
		response.HandleError(c, appErr)
		return
	}
	switch {
	case errors.Is(err, domain.ErrInvalidCustomField):
		response.HandleError(c, withFieldDetails(httperrors.BadRequest("INVALID_CUSTOM_FIELD", err.Error(), err), err, "value"))
	case errors.Is(err, domain.ErrCustomFieldInUse):
		response.HandleError(c, httperrors.Conflict("CUSTOM_FIELD_IN_USE", err.Error(), err))
	case errors.Is(err, domain.ErrCustomFieldNameTaken):
		response.HandleError(c, httperrors.Conflict("CUSTOM_FIELD_NAME_TAKEN", err.Error(), err))
	case errors.Is(err, domain.ErrNotFound):
		response.HandleError(c, httperrors.NotFound("CUSTOM_FIELD_NOT_FOUND", "custom field or task not found", err))
	default:
		response.HandleError(c, httperrors.Internal(code, "internal server error", err))
	}
}

func customFieldInput(req dto.CustomFieldRequest) service.CustomFieldInput {
	options := make([]domain.CustomFieldOption, len(req.Options))
	for i, option := range req.Options {
		options[i] = domain.CustomFieldOption{Label: option.Label, Value: option.Value, Position: i}
	}
	return service.CustomFieldInput{
		Name: req.Name, Description: req.Description, Type: domain.CustomFieldType(req.Type),
		Required: req.Required, Config: req.Config, Options: options,
	}
}

func parseCustomFieldRouteIDs(c *gin.Context) (uint, uint, bool) {
	workspaceID, ok := parseCustomFieldID(c, "workspace_id")
	if !ok {
		return 0, 0, false
	}
	fieldID, ok := parseCustomFieldID(c, "field_id")
	return workspaceID, fieldID, ok
}

func parseTaskCustomFieldRouteIDs(c *gin.Context) (uint, uint, bool) {
	taskID, ok := parseCustomFieldID(c, "task_id")
	if !ok {
		return 0, 0, false
	}
	fieldID, ok := parseCustomFieldID(c, "field_id")
	return taskID, fieldID, ok
}

func parseCustomFieldID(c *gin.Context, param string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(param), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_ID", "invalid "+param, err))
		return 0, false
	}
	return uint(id), true
}
