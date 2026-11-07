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

type TaskTemplateHandler struct {
	service     service.TaskTemplateService
	permissions service.PermissionChecker
	applier     service.TaskTemplateApplier
}

func NewTaskTemplateHandler(
	svc service.TaskTemplateService,
	permissions service.PermissionChecker,
	applier service.TaskTemplateApplier,
) *TaskTemplateHandler {
	return &TaskTemplateHandler{
		service: svc, permissions: permissions, applier: applier,
	}
}

func (h *TaskTemplateHandler) Create(c *gin.Context) {
	workspaceID, ok := parseTemplateID(c, "workspace_id")
	if !ok {
		return
	}
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	var req dto.TaskTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	template, err := h.service.Create(c.Request.Context(), workspaceID, userID,
		req.Name, req.Description, req.Configuration)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, template)
}

func (h *TaskTemplateHandler) List(c *gin.Context) {
	workspaceID, ok := parseTemplateID(c, "workspace_id")
	if !ok {
		return
	}
	templates, err := h.service.List(c.Request.Context(), workspaceID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, templates)
}

func (h *TaskTemplateHandler) Get(c *gin.Context) {
	template, ok := h.authorizedTemplate(c, domain.PermWorkspaceRead)
	if !ok {
		return
	}
	response.Success(c, http.StatusOK, template)
}

func (h *TaskTemplateHandler) Update(c *gin.Context) {
	template, ok := h.authorizedTemplate(c, domain.PermWorkspaceUpdate)
	if !ok {
		return
	}
	var req dto.UpdateTaskTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	updated, err := h.service.Update(c.Request.Context(), template.ID, req.Name, req.Description)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, updated)
}

func (h *TaskTemplateHandler) Archive(c *gin.Context) {
	template, ok := h.authorizedTemplate(c, domain.PermWorkspaceUpdate)
	if !ok {
		return
	}
	if err := h.service.Archive(c.Request.Context(), template.ID); err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "task template archived"})
}

func (h *TaskTemplateHandler) CreateVersion(c *gin.Context) {
	template, ok := h.authorizedTemplate(c, domain.PermWorkspaceUpdate)
	if !ok {
		return
	}
	var req dto.TaskTemplateVersionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	version, err := h.service.CreateVersion(c.Request.Context(), template.ID, req.Configuration)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, version)
}

func (h *TaskTemplateHandler) ListVersions(c *gin.Context) {
	template, ok := h.authorizedTemplate(c, domain.PermWorkspaceRead)
	if !ok {
		return
	}
	versions, err := h.service.ListVersions(c.Request.Context(), template.ID)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusOK, versions)
}

func (h *TaskTemplateHandler) Apply(c *gin.Context) {
	template, ok := h.authorizedTemplate(c, domain.PermTaskCreate)
	if !ok {
		return
	}
	var req dto.ApplyTaskTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	task, err := h.applier.Apply(c.Request.Context(), userID, template.ID, req.ListID, req.StatusID,
		req.Title, req.Description)
	if err != nil {
		h.writeError(c, err)
		return
	}
	response.Success(c, http.StatusCreated, task)
}

func (h *TaskTemplateHandler) authorizedTemplate(c *gin.Context, permission string) (*domain.TaskTemplate, bool) {
	templateID, ok := parseTemplateID(c, "template_id")
	if !ok {
		return nil, false
	}
	template, err := h.service.Get(c.Request.Context(), templateID)
	if err != nil {
		h.writeError(c, err)
		return nil, false
	}
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return nil, false
	}
	allowed, err := h.permissions.HasWorkspacePermission(c.Request.Context(), userID, template.WorkspaceID, permission)
	if err != nil {
		response.HandleError(c, httperrors.Internal("TASK_TEMPLATE_PERMISSION_FAILED", "internal server error", err))
		return nil, false
	}
	if !allowed {
		response.Forbidden(c, "")
		return nil, false
	}
	return template, true
}

func (h *TaskTemplateHandler) writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidTaskTemplate):
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_TEMPLATE", err.Error(), err))
	case errors.Is(err, domain.ErrTaskTemplateNotFound), errors.Is(err, domain.ErrNotFound):
		response.HandleError(c, httperrors.NotFound("TASK_TEMPLATE_NOT_FOUND", "task template not found", err))
	case errors.Is(err, domain.ErrTaskTemplateArchived):
		response.HandleError(c, httperrors.Conflict("TASK_TEMPLATE_ARCHIVED", err.Error(), err))
	case errors.Is(err, domain.ErrInvalidListReference), errors.Is(err, domain.ErrInvalidStatusReference),
		errors.Is(err, domain.ErrInvalidAssigneeReference), errors.Is(err, domain.ErrInvalidTagReference),
		errors.Is(err, domain.ErrInvalidCustomField):
		response.HandleError(c, httperrors.BadRequest("INVALID_TASK_TEMPLATE_APPLICATION", err.Error(), err))
	default:
		response.HandleError(c, httperrors.Internal("TASK_TEMPLATE_FAILED", "internal server error", err))
	}
}

func parseTemplateID(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_ID", "invalid "+name, err))
		return 0, false
	}
	return uint(id), true
}

func decodeFormTaskConfiguration(req dto.FormTaskConfigurationRequest) domain.FormTaskConfiguration {
	config := domain.FormTaskConfiguration{
		Enabled: req.Enabled,
		Target: domain.FormTaskTarget{
			WorkspaceID: req.Target.WorkspaceID, ListID: req.Target.ListID,
		},
		TaskDefaults: domain.FormTaskDefaults{
			Priority: req.TaskDefaults.Priority, StatusID: req.TaskDefaults.StatusID,
			AssigneeID: req.TaskDefaults.AssigneeID,
		},
		Mappings: make([]domain.FormTaskMapping, len(req.Mappings)),
	}
	if req.Template != nil {
		config.Template = &domain.FormTaskTemplate{
			TemplateID: req.Template.TemplateID, VersionID: req.Template.VersionID,
		}
	}
	for i, mapping := range req.Mappings {
		config.Mappings[i] = domain.FormTaskMapping{
			QuestionID: mapping.QuestionID,
			Target: domain.FormMappingTarget{
				Type: mapping.Target.Type, Field: mapping.Target.Field, FieldID: mapping.Target.FieldID,
			},
		}
		if mapping.Transform != nil {
			config.Mappings[i].Transform = &domain.FormValueTransform{
				Type: mapping.Transform.Type, Values: mapping.Transform.Values,
			}
		}
	}
	return config
}
