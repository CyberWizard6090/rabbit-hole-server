package handler

import (
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

type StatusHandler struct {
	service service.StatusService
}

const invalidListIDMessage = "invalid list id"

func NewStatusHandler(
	service service.StatusService,
) *StatusHandler {

	return &StatusHandler{
		service: service,
	}
}

func (h *StatusHandler) Create(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", invalidListIDMessage, err))
		return
	}

	var req dto.CreateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	status, err := h.service.Create(c.Request.Context(), service.CreateStatusParams{
		ListID:   uint(listID),
		Name:     req.Name,
		Color:    req.Color,
		Position: req.Position,
		Type:     req.Type,
	})
	if err != nil {
		response.HandleError(c, httperrors.Internal("STATUS_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, status)
}

func (h *StatusHandler) List(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", invalidListIDMessage, err))
		return
	}

	statuses, err := h.service.GetAllByList(c.Request.Context(), uint(listID))
	if err != nil {
		response.HandleError(c, httperrors.Internal("STATUSES_FETCH_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, statuses)
}

func (h *StatusHandler) Update(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", invalidListIDMessage, err))
		return
	}
	statusID, err := strconv.ParseUint(c.Param("status_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_STATUS_ID", "invalid status id", err))
		return
	}

	var req dto.UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	status, err := h.service.Update(c.Request.Context(), uint(listID), uint(statusID), service.UpdateStatusParams{
		Name:     req.Name,
		Color:    req.Color,
		Position: req.Position,
		Type:     req.Type,
	})
	if err != nil {
		response.HandleError(c, httperrors.Internal("STATUS_UPDATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, status)
}

func (h *StatusHandler) Delete(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", invalidListIDMessage, err))
		return
	}
	statusID, err := strconv.ParseUint(c.Param("status_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_STATUS_ID", "invalid status id", err))
		return
	}

	replacementID, ok := parseReplacementStatusID(c)
	if !ok {
		return
	}

	if err := h.service.Delete(c.Request.Context(), uint(listID), uint(statusID), replacementID); err != nil {
		var usedByForm *domain.StatusUsedByFormError
		switch {
		case errors.As(err, &usedByForm):
			appErr := httperrors.Conflict("STATUS_USED_BY_FORM", err.Error(), err)
			response.HandleError(c, appErr.WithParams(map[string]any{"form_ids": usedByForm.FormIDs}))
		case errors.Is(err, domain.ErrStatusInUse):
			response.HandleError(c, httperrors.Conflict("STATUS_IN_USE", err.Error(), err))
		case errors.Is(err, domain.ErrInvalidReplacementStatus):
			response.HandleError(c, httperrors.BadRequest("INVALID_REPLACEMENT_STATUS", err.Error(), err))
		case errors.Is(err, domain.ErrNotFound):
			response.HandleError(c, httperrors.NotFound("STATUS_NOT_FOUND", "status not found", err))
		default:
			response.HandleError(c, httperrors.Internal("STATUS_DELETE_FAILED", "internal server error", err))
		}
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "status deleted"})
}

func parseReplacementStatusID(c *gin.Context) (*uint, bool) {
	raw := c.Query("replacement_status_id")
	legacy := c.Query("replacement_id")
	if raw != "" && legacy != "" && raw != legacy {
		response.HandleError(c, httperrors.BadRequest("INVALID_REPLACEMENT_ID", "replacement_status_id and replacement_id differ", nil))
		return nil, false
	}
	if raw == "" {
		raw = legacy
	}
	if raw == "" {
		return nil, true
	}
	parsed, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || parsed == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_REPLACEMENT_ID", "invalid replacement_status_id", err))
		return nil, false
	}
	id := uint(parsed)
	return &id, true
}
