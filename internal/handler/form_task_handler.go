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

type FormTaskHandler struct {
	config      service.FormTaskConfigurationService
	forms       domain.FormRepository
	processor   domain.FormTaskProcessor
	tasks       service.TaskService
	permissions service.PermissionChecker
}

func NewFormTaskHandler(
	config service.FormTaskConfigurationService,
	forms domain.FormRepository,
	processor domain.FormTaskProcessor,
	tasks service.TaskService,
	permissions service.PermissionChecker,
) *FormTaskHandler {
	return &FormTaskHandler{
		config: config, forms: forms, processor: processor, tasks: tasks, permissions: permissions,
	}
}

func (h *FormTaskHandler) GetConfig(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	config, err := h.config.Get(c.Request.Context(), workspaceID, formID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, config)
}

func (h *FormTaskHandler) SetConfig(c *gin.Context) {
	workspaceID, formID, ok := parseFormRouteIDs(c)
	if !ok {
		return
	}
	var req dto.FormTaskConfigurationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	config := decodeFormTaskConfiguration(req)
	if err := h.config.Set(c.Request.Context(), workspaceID, formID, config); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, config)
}

func (h *FormTaskHandler) ListSubmissionTasks(c *gin.Context) {
	submissionID, ok := parseSubmissionID(c)
	if !ok || !h.authorizeSubmission(c, submissionID, domain.PermWorkspaceRead) {
		return
	}
	taskID, err := h.forms.GetSubmissionTask(c.Request.Context(), submissionID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	task, err := h.tasks.GetTaskByID(c.Request.Context(), taskID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, []domain.Task{*task})
}

func (h *FormTaskHandler) TaskStatus(c *gin.Context) {
	submissionID, ok := parseSubmissionID(c)
	if !ok || !h.authorizeSubmission(c, submissionID, domain.PermWorkspaceRead) {
		return
	}
	status, taskError, err := h.forms.GetSubmissionTaskStatus(c.Request.Context(), submissionID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"status": status, "error": taskError})
}

func (h *FormTaskHandler) Retry(c *gin.Context) {
	submissionID, ok := parseSubmissionID(c)
	if !ok || !h.authorizeSubmission(c, submissionID, domain.PermWorkspaceUpdate) {
		return
	}
	status, err := h.processor.RetrySubmission(c.Request.Context(), submissionID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	_, taskError, err := h.forms.GetSubmissionTaskStatus(c.Request.Context(), submissionID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"status": status, "error": taskError})
}

func (h *FormTaskHandler) authorizeSubmission(c *gin.Context, submissionID uint, permission string) bool {
	workspaceID, err := h.forms.GetSubmissionWorkspace(c.Request.Context(), submissionID)
	if err != nil {
		h.writeError(c, err)
		return false
	}
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return false
	}
	allowed, err := h.permissions.HasWorkspacePermission(c.Request.Context(), userID, workspaceID, permission)
	if err != nil {
		response.HandleError(c, httperrors.Internal("SUBMISSION_PERMISSION_FAILED", "internal server error", err))
		return false
	}
	if !allowed {
		response.Forbidden(c, "")
		return false
	}
	return true
}

func (h *FormTaskHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidForm), errors.Is(err, domain.ErrInvalidSubmission),
		errors.Is(err, domain.ErrInvalidTaskTemplate):
		response.HandleError(c, httperrors.BadRequest("INVALID_FORM_TASK_CONFIG", err.Error(), err))
	case errors.Is(err, domain.ErrFormNotFound), errors.Is(err, domain.ErrNotFound):
		response.HandleError(c, httperrors.NotFound("SUBMISSION_OR_FORM_NOT_FOUND", "submission or form not found", err))
	case errors.Is(err, domain.ErrTaskTemplateNotFound):
		response.HandleError(c, httperrors.NotFound("TASK_TEMPLATE_NOT_FOUND", "task template not found", err))
	default:
		response.HandleError(c, httperrors.Internal("FORM_TASK_FAILED", "internal server error", err))
	}
}

func parseSubmissionID(c *gin.Context) (uint, bool) {
	value, err := strconv.ParseUint(c.Param("submission_id"), 10, 64)
	if err != nil || value == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_ID", "invalid submission_id", err))
		return 0, false
	}
	return uint(value), true
}
