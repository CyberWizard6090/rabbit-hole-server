package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type TaskRelationHandler struct {
	service *service.TaskRelationService
}

func NewTaskRelationHandler(svc *service.TaskRelationService) *TaskRelationHandler {
	return &TaskRelationHandler{service: svc}
}

func (h *TaskRelationHandler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrRelationSelf):
		response.HandleError(c, httperrors.BadRequest("RELATION_SELF", err.Error(), err).
			WithDetails(httperrors.Detail{Field: "task_id", Code: "SAME_TASK"}))
	case errors.Is(err, domain.ErrRelationOtherTask):
		response.HandleError(c, httperrors.NotFound("TASK_NOT_FOUND", "task not found", err))
	case errors.Is(err, domain.ErrRelationCycle):
		response.HandleError(c, httperrors.Conflict("RELATION_CYCLE", err.Error(), err))
	case errors.Is(err, domain.ErrRelationExists):
		response.HandleError(c, httperrors.Conflict("RELATION_EXISTS", err.Error(), err))
	case errors.Is(err, domain.ErrRelationLimit):
		response.HandleError(c, httperrors.Conflict("RELATION_LIMIT_REACHED", err.Error(), err).
			WithParams(map[string]any{"max": domain.MaxRelationsPerTask}))
	case errors.Is(err, domain.ErrRelationMissing):
		response.HandleError(c, httperrors.NotFound("RELATION_NOT_FOUND", err.Error(), err))
	case errors.Is(err, domain.ErrRelationBadKind):
		response.HandleError(c, httperrors.BadRequest("VALIDATION_FAILED", "invalid request", err).
			WithDetails(httperrors.Detail{Field: "kind", Code: "INVALID_CHOICE"}))
	default:
		response.HandleError(c, httperrors.Internal("RELATION_FAILED", "internal server error", err))
	}
}

func (h *TaskRelationHandler) List(c *gin.Context) {
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	relations, err := h.service.List(c.Request.Context(), taskID)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusOK, relations)
}

func (h *TaskRelationHandler) Add(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	var req dto.AddRelationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.service.Add(c.Request.Context(), uid, taskID, req.TaskID, req.Kind); err != nil {
		h.fail(c, err)
		return
	}
	relations, err := h.service.List(c.Request.Context(), taskID)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusCreated, relations)
}

func (h *TaskRelationHandler) Remove(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	taskID, ok := taskIDParam(c)
	if !ok {
		return
	}
	otherID, err := strconv.ParseUint(c.Param("other_id"), 10, 64)
	if err != nil || otherID == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_ID", invalidTaskIDMessage, err))
		return
	}
	kind := c.Query("kind")
	if kind == "" {
		response.HandleError(c, httperrors.BadRequest("VALIDATION_FAILED", "invalid request", nil).
			WithDetails(httperrors.Detail{Field: "kind", Code: "REQUIRED"}))
		return
	}
	if err := h.service.Remove(c.Request.Context(), uid, taskID, uint(otherID), kind); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "relation removed"})
}
