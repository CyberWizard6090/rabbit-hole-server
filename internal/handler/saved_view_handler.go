package handler

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/http/taskquery"
	"rabbit-hole-server/internal/service"
)

type SavedViewHandler struct {
	service *service.SavedViewService
}

func NewSavedViewHandler(svc *service.SavedViewService) *SavedViewHandler {
	return &SavedViewHandler{service: svc}
}

var allowedViewFilters = map[string]struct{}{
	"q": {}, "status_id": {}, "priority": {}, "tag_id": {}, "no_tags": {}, "assignee_id": {}, "unassigned": {},
	"created_from": {}, "created_to": {}, "due_from": {}, "due_to": {}, "start_from": {}, "start_to": {},
	"has_due": {}, "parent_id": {}, "has_parent": {}, "has_open_checklist_items": {},
}

func validateViewConfig(filters map[string]string, sort string) *httperrors.AppError {
	values := url.Values{}
	for key, value := range filters {
		if _, ok := allowedViewFilters[key]; !ok {
			return httperrors.BadRequest("INVALID_FILTER", "unknown filter", nil).
				WithDetails(httperrors.Detail{Field: "filters." + key, Code: "UNKNOWN_FILTER"})
		}
		values.Set(key, value)
	}
	if sort != "" {
		values.Set("sort", sort)
	}
	if _, appErr := taskquery.Parse(values); appErr != nil {
		return appErr
	}
	return nil
}

func viewID(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("view_id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_VIEW_ID", "invalid view id", err))
		return 0, false
	}
	return uint(id), true
}

func (h *SavedViewHandler) fail(c *gin.Context, err error) {
	var fieldErr *domain.FieldError
	switch {
	case errors.Is(err, domain.ErrViewNotFound):
		response.HandleError(c, httperrors.NotFound("VIEW_NOT_FOUND", "view not found", err))
	case errors.Is(err, domain.ErrViewForbidden):
		response.HandleError(c, httperrors.Forbidden("VIEW_FORBIDDEN", "you cannot change this view", err))
	case errors.Is(err, domain.ErrViewLimitReached):
		response.HandleError(c, httperrors.Conflict("VIEW_LIMIT_REACHED", err.Error(), err))
	case errors.As(err, &fieldErr):
		response.HandleError(c, httperrors.BadRequest("VALIDATION_FAILED", "invalid request", err).WithDetails(fieldDetails(fieldErr)...))
	default:
		response.HandleError(c, httperrors.Internal("VIEW_FAILED", "internal server error", err))
	}
}

func fieldDetails(err *domain.FieldError) []httperrors.Detail {
	out := make([]httperrors.Detail, 0, len(err.Problems))
	for _, p := range err.Problems {
		out = append(out, httperrors.Detail{Field: p.Field, Code: p.Reason, Params: p.Params})
	}
	return out
}

func (h *SavedViewHandler) List(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := workspaceIDParam(c)
	if !ok {
		return
	}
	var listID *uint
	if raw := c.Query("list_id"); raw != "" {
		id, parseErr := strconv.ParseUint(raw, 10, 64)
		if parseErr != nil || id == 0 {
			response.HandleError(c, httperrors.BadRequest("INVALID_FILTER", "invalid list id", parseErr).
				WithDetails(httperrors.Detail{Field: "list_id", Code: "INVALID_VALUE"}))
			return
		}
		value := uint(id)
		listID = &value
	}
	views, err := h.service.List(c.Request.Context(), workspaceID, uid, listID)
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusOK, views)
}

func (h *SavedViewHandler) Create(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := workspaceIDParam(c)
	if !ok {
		return
	}
	var req dto.SavedViewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if appErr := validateViewConfig(req.Filters, req.Sort); appErr != nil {
		response.HandleError(c, appErr)
		return
	}
	view, err := h.service.Create(c.Request.Context(), workspaceID, uid, service.SavedViewInput{
		Name: req.Name, ListID: req.ListID, Shared: req.Shared, Filters: req.Filters, Sort: req.Sort,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusCreated, view)
}

func (h *SavedViewHandler) Update(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := workspaceIDParam(c)
	if !ok {
		return
	}
	id, ok := viewID(c)
	if !ok {
		return
	}
	var req dto.UpdateSavedViewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	sortValue := ""
	if req.Sort != nil {
		sortValue = *req.Sort
	}
	if req.Filters != nil || req.Sort != nil {
		if appErr := validateViewConfig(req.Filters, sortValue); appErr != nil {
			response.HandleError(c, appErr)
			return
		}
	}
	view, err := h.service.Update(c.Request.Context(), workspaceID, uid, id, service.SavedViewPatch{
		Name: req.Name, Shared: req.Shared, Filters: req.Filters, Sort: req.Sort,
	})
	if err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusOK, view)
}

func (h *SavedViewHandler) Delete(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := workspaceIDParam(c)
	if !ok {
		return
	}
	id, ok := viewID(c)
	if !ok {
		return
	}
	if err := h.service.Delete(c.Request.Context(), workspaceID, uid, id); err != nil {
		h.fail(c, err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "view deleted"})
}
