package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type SpaceHandler struct {
	service service.SpaceService
}

func NewSpaceHandler(spaceService service.SpaceService) *SpaceHandler {
	return &SpaceHandler{
		service: spaceService,
	}
}

func (h *SpaceHandler) Create(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, err := strconv.ParseUint(c.Param("workspace_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_WORKSPACE_ID", "invalid workspace id", err))
		return
	}

	var req dto.CreateSpaceRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	if req.Currency == "" {
		req.Currency = "USD"
	}

	space, err := h.service.CreateSpace(
		uint(workspaceID),
		req.Name,
		req.Currency,
		uid,
	)

	if err != nil {
		response.HandleError(c, httperrors.Internal("SPACE_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, space)
}

func spaceIDParam(c *gin.Context) (uint, bool) {
	id, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_SPACE_ID", "invalid space id", err))
		return 0, false
	}
	return uint(id), true
}

func (h *SpaceHandler) Get(c *gin.Context) {
	id, ok := spaceIDParam(c)
	if !ok {
		return
	}
	space, err := h.service.GetSpaceByID(id)
	if err != nil {
		response.HandleError(c, httperrors.NotFound("SPACE_NOT_FOUND", "space not found", err))
		return
	}
	response.Success(c, http.StatusOK, space)
}

func (h *SpaceHandler) List(c *gin.Context) {
	workspaceID, err := strconv.ParseUint(c.Param("workspace_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_WORKSPACE_ID", "invalid workspace id", err))
		return
	}

	p, err := pagination.ParseOffset(c.Query("page"), c.Query("limit"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", err.Error(), err))
		return
	}

	spaces, total, err := h.service.GetAllSpaces(uint(workspaceID), p.Limit, p.Offset)
	if err != nil {
		response.HandleError(c, httperrors.Internal("SPACES_FETCH_FAILED", "internal server error", err))
		return
	}

	pages := (int(total) + p.Limit - 1) / p.Limit

	response.SuccessWithPagination(c, http.StatusOK, spaces, &response.Pagination{
		Total: int(total), Page: p.Page, Limit: p.Limit, Pages: pages,
	})
}

func (h *SpaceHandler) Update(c *gin.Context) {
	id, ok := spaceIDParam(c)
	if !ok {
		return
	}

	var req dto.UpdateSpaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	space, err := h.service.UpdateSpace(id, req.Name, req.Currency)
	if err != nil {
		if errors.Is(err, service.ErrSpaceNameRequired) {
			response.HandleError(c, httperrors.BadRequest("SPACE_NAME_REQUIRED", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Internal("SPACE_UPDATE_FAILED", "internal server error", err))
		return
	}
	response.Success(c, http.StatusOK, space)
}

func (h *SpaceHandler) Delete(c *gin.Context) {
	id, ok := spaceIDParam(c)
	if !ok {
		return
	}
	if err := h.service.DeleteSpace(id); err != nil {
		response.HandleError(c, httperrors.Internal("SPACE_DELETE_FAILED", "internal server error", err))
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "space deleted"})
}

func (h *SpaceHandler) GetDashboard(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_SPACE_ID", "invalid space id", err))
		return
	}

	dashboard, err := h.service.GetDashboard(uint(spaceID))
	if err != nil {
		response.HandleError(c, httperrors.NotFound("SPACE_NOT_FOUND", "space not found", err))
		return
	}

	response.Success(c, http.StatusOK, dashboard)
}
