package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
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

func (h *SpaceHandler) CreateSpace(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, err := strconv.ParseUint(c.Param("workspace_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid workspace id")
		return
	}

	var req dto.CreateSpaceRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
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
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusCreated, space)
}

func (h *SpaceHandler) GetAll(c *gin.Context) {
	workspaceID, err := strconv.ParseUint(c.Param("workspace_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid workspace id")
		return
	}

	p := pagination.Parse(c)

	spaces, total, err := h.service.GetAllSpaces(uint(workspaceID), p.Limit, p.Offset)
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	pages := (int(total) + p.Limit - 1) / p.Limit

	response.SuccessWithPagination(c, http.StatusOK, spaces, &response.Pagination{
		Total: int(total), Page: p.Page, Limit: p.Limit, Pages: pages,
	})
}

func (h *SpaceHandler) GetDashboard(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid space id")
		return
	}

	dashboard, err := h.service.GetDashboard(uint(spaceID))
	if err != nil {
		log.Println(err)
		response.NotFound(c, "space not found")
		return
	}

	response.Success(c, http.StatusOK, dashboard)
}
