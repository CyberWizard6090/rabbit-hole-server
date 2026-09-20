package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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

	status, err := h.service.Create(service.CreateStatusParams{
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

func (h *StatusHandler) GetAll(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", invalidListIDMessage, err))
		return
	}

	statuses, err := h.service.GetAllByList(uint(listID))
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

	status, err := h.service.Update(uint(listID), uint(statusID), service.UpdateStatusParams{
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

	if err := h.service.Delete(uint(listID), uint(statusID)); err != nil {
		response.HandleError(c, httperrors.Internal("STATUS_DELETE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "status deleted"})
}
