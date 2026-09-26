package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
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
			response.HandleError(c, httperrors.BadRequest("WORKSPACE_NAME_REQUIRED", err.Error(), err))
			return
		}

		response.HandleError(c, httperrors.Internal("WORKSPACE_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(
		c,
		http.StatusCreated,
		workspace,
	)
}

func (h *WorkspaceHandler) List(c *gin.Context) {
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	workspaces, err := h.service.GetAllForUser(userID)
	if err != nil {
		response.HandleError(c, httperrors.Internal("WORKSPACES_FETCH_FAILED", "internal server error", err))
		return
	}

	response.Success(
		c,
		http.StatusOK,
		workspaces,
	)
}

func (h *WorkspaceHandler) Get(c *gin.Context) {
	// TODO:  Добавить функцию получения рабочего пространства по ID
}

func (h *WorkspaceHandler) Update(c *gin.Context) {

	// TODO: Добавить функцию обновления рабочего пространства
}
func (h *WorkspaceHandler) Delete(c *gin.Context) {
	// TODO:  Добавить функцию удаления рабочего пространства
}
