package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type ActivityHandler struct {
	service service.ActivityService
}

func NewActivityHandler(s service.ActivityService) *ActivityHandler {
	return &ActivityHandler{service: s}
}

func (h *ActivityHandler) List(c *gin.Context) {
	taskID, ok := parseTaskID(c)
	if !ok {
		return
	}
	params, err := pagination.ParseOffset(c.Query("page"), c.Query("limit"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", err.Error(), err))
		return
	}

	items, total, err := h.service.ListActivity(c.Request.Context(), taskID, params.Limit, params.Offset)
	if err != nil {
		response.HandleError(c, httperrors.Internal("ACTIVITY_FETCH_FAILED", "internal server error", err))
		return
	}
	pages := (int(total) + params.Limit - 1) / params.Limit
	response.SuccessWithPagination(c, http.StatusOK, items, &response.Pagination{
		Total: int(total), Page: params.Page, Limit: params.Limit, Pages: pages,
	})
}
