package handler

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type WorkspaceHandler struct {
	service service.WorkspaceService
}

func NewWorkspaceHandler(
	workspaceService service.WorkspaceService,
) *WorkspaceHandler {
	return &WorkspaceHandler{
		service: workspaceService,
	}
}

func (h *WorkspaceHandler) Create(c *gin.Context) {
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	var req dto.CreateWorkspaceRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	workspace, err := h.service.CreateWorkspace(
		userID,
		req.Name,
		req.Description,
	)
	if err != nil {
		if errors.Is(err, service.ErrWorkspaceNameRequired) {
			response.BadRequest(c, err.Error())
			return
		}

		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(
		c,
		http.StatusCreated,
		workspace,
	)
}

func (h *WorkspaceHandler) GetAllForUser(c *gin.Context) {
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	workspaces, err := h.service.GetAllForUser(userID)
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(
		c,
		http.StatusOK,
		workspaces,
	)
}
