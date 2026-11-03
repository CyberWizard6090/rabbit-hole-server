package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
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
		response.ValidationError(c, err)
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

func parseUintParam(c *gin.Context, name string) (uint, bool) {
	id, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || id == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_"+strings.ToUpper(name), "invalid "+name, err))
		return 0, false
	}
	return uint(id), true
}

func memberError(c *gin.Context, code string, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidMemberRole):
		response.HandleError(c, httperrors.BadRequest("INVALID_ROLE", err.Error(), err))
	case errors.Is(err, service.ErrMemberNotContact):
		response.HandleError(c, httperrors.UnprocessableEntity("NOT_A_CONTACT", err.Error(), err))
	case errors.Is(err, domain.ErrAlreadyMember):
		response.HandleError(c, httperrors.Conflict("ALREADY_MEMBER", err.Error(), err))
	case errors.Is(err, service.ErrOwnerImmutable):
		response.HandleError(c, httperrors.Forbidden("OWNER_IMMUTABLE", err.Error(), err))
	default:
		response.HandleError(c, httperrors.Internal(code, "internal server error", err))
	}
}

func (h *WorkspaceHandler) ListMembers(c *gin.Context) {
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}

	members, err := h.service.ListMembers(workspaceID)
	if err != nil {
		memberError(c, "MEMBERS_FETCH_FAILED", err)
		return
	}

	result := make([]dto.MemberResponse, 0, len(members))
	for _, m := range members {
		result = append(result, dto.MemberResponse{
			UserID: m.UserID, Username: m.Username, FirstName: m.FirstName,
			LastName: m.LastName, AvatarURL: m.AvatarURL, Role: m.Role,
		})
	}
	response.Success(c, http.StatusOK, result)
}

func (h *WorkspaceHandler) AddMember(c *gin.Context) {
	actorID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}

	var req dto.AddMemberRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	if err := h.service.AddMember(workspaceID, actorID, req.UserID, req.Role); err != nil {
		memberError(c, "MEMBER_ADD_FAILED", err)
		return
	}
	response.Success(c, http.StatusCreated, gin.H{"message": "member added"})
}

func (h *WorkspaceHandler) UpdateMemberRole(c *gin.Context) {
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}
	targetID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}

	var req dto.UpdateMemberRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	if err := h.service.ChangeMemberRole(workspaceID, targetID, req.Role); err != nil {
		memberError(c, "MEMBER_UPDATE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "member role updated"})
}

func (h *WorkspaceHandler) RemoveMember(c *gin.Context) {
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}
	targetID, ok := parseUintParam(c, "user_id")
	if !ok {
		return
	}

	if err := h.service.RemoveMember(workspaceID, targetID); err != nil {
		memberError(c, "MEMBER_REMOVE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "member removed"})
}

func (h *WorkspaceHandler) Leave(c *gin.Context) {
	userID, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	workspaceID, ok := parseUintParam(c, "workspace_id")
	if !ok {
		return
	}

	if err := h.service.RemoveMember(workspaceID, userID); err != nil {
		memberError(c, "MEMBER_LEAVE_FAILED", err)
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "left workspace"})
}
