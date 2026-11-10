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

type ListHandler struct {
	service       service.SpaceService
	folderService service.FolderService
}

func NewListHandler(s service.SpaceService, fs service.FolderService) *ListHandler {
	return &ListHandler{service: s, folderService: fs}
}

func (h *ListHandler) CreateInSpace(c *gin.Context) {
	if _, err := contextutil.GetUserID(c); err != nil {
		response.Unauthorized(c)
		return
	}

	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_SPACE_ID", "invalid space id", err))
		return
	}

	var req dto.CreateListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	list, err := h.service.CreateList(service.WithLocale(c.Request.Context(), c.GetHeader("Accept-Language")), uint(spaceID), req.Name, req.ParentStatusID, req.Template)
	if err != nil {
		if errors.Is(err, service.ErrUnknownListTemplate) {
			unknownTemplate(c, err)
			return
		}
		if errors.Is(err, domain.ErrParentStatusNotFound) {
			response.HandleError(c, httperrors.BadRequest("VALIDATION_FAILED", err.Error(), err).
				WithDetails(httperrors.Detail{Field: "parent_status_id", Code: "NOT_FOUND"}))
			return
		}
		response.HandleError(c, httperrors.Internal("LIST_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, list)
}

func (h *ListHandler) ListInSpace(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_SPACE_ID", "invalid space id", err))
		return
	}

	space, err := h.service.GetSpaceByID(c.Request.Context(), uint(spaceID))
	if err != nil {
		response.HandleError(c, httperrors.NotFound("SPACE_NOT_FOUND", "space not found", err))
		return
	}

	response.Success(c, http.StatusOK, space.Lists)
}

func (h *ListHandler) CreateInFolder(c *gin.Context) {
	if _, err := contextutil.GetUserID(c); err != nil {
		response.Unauthorized(c)
		return
	}

	folderID, err := strconv.ParseUint(c.Param("folder_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_FOLDER_ID", "invalid folder id", err))
		return
	}

	folder, err := h.folderService.GetFolderByID(c.Request.Context(), uint(folderID))
	if err != nil {
		response.HandleError(c, httperrors.NotFound("FOLDER_NOT_FOUND", "folder not found", err))
		return
	}

	var req dto.CreateListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	list, err := h.service.CreateListInFolder(service.WithLocale(c.Request.Context(), c.GetHeader("Accept-Language")), folder.SpaceID, uint(folderID), req.Name, req.Template)
	if err != nil {
		if errors.Is(err, service.ErrUnknownListTemplate) {
			unknownTemplate(c, err)
			return
		}
		response.HandleError(c, httperrors.Internal("LIST_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, list)
}

func (h *ListHandler) ListInFolder(c *gin.Context) {
	folderID, err := strconv.ParseUint(c.Param("folder_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_FOLDER_ID", "invalid folder id", err))
		return
	}

	lists, err := h.service.GetListsByFolder(c.Request.Context(), uint(folderID))
	if err != nil {
		response.HandleError(c, httperrors.Internal("LISTS_FETCH_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, lists)
}

func (h *ListHandler) Update(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", "invalid list id", err))
		return
	}

	var req dto.UpdateListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	list, err := h.service.UpdateList(c.Request.Context(), uint(listID), req.Name, req.FolderID)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidFolderReference):
			response.HandleError(c, httperrors.BadRequest("INVALID_FOLDER_REFERENCE", err.Error(), err))
		case errors.Is(err, domain.ErrNotFound):
			response.HandleError(c, httperrors.NotFound("LIST_NOT_FOUND", "list not found", err))
		default:
			response.HandleError(c, httperrors.Internal("LIST_UPDATE_FAILED", "internal server error", err))
		}
		return
	}

	response.Success(c, http.StatusOK, list)
}

func (h *ListHandler) Delete(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_LIST_ID", "invalid list id", err))
		return
	}

	if err := h.service.DeleteList(c.Request.Context(), uint(listID)); err != nil {
		response.HandleError(c, httperrors.Internal("LIST_DELETE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "list deleted"})
}

func unknownTemplate(c *gin.Context, err error) {
	response.HandleError(c, httperrors.BadRequest("UNKNOWN_LIST_TEMPLATE", err.Error(), err).
		WithDetails(httperrors.Detail{Field: "template", Code: "INVALID_CHOICE"}))
}

func (h *ListHandler) Templates(c *gin.Context) {
	locale := service.LocaleFromAcceptLanguage(c.GetHeader("Accept-Language"))
	response.Success(c, http.StatusOK, domain.ListTemplates(locale))
}
