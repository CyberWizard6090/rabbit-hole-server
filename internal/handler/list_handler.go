package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

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

	list, err := h.service.CreateList(uint(spaceID), req.Name, req.ParentStatusID)
	if err != nil {
		log.Println(err)
		response.HandleError(c, httperrors.Internal("LIST_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, list)
}

func (h *ListHandler) GetAllInSpace(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_SPACE_ID", "invalid space id", err))
		return
	}

	space, err := h.service.GetSpaceByID(uint(spaceID))
	if err != nil {
		log.Println(err)
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

	folder, err := h.folderService.GetFolderByID(uint(folderID))
	if err != nil {
		response.HandleError(c, httperrors.NotFound("FOLDER_NOT_FOUND", "folder not found", err))
		return
	}

	var req dto.CreateListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	list, err := h.service.CreateListInFolder(folder.SpaceID, uint(folderID), req.Name)
	if err != nil {
		log.Println(err)
		response.HandleError(c, httperrors.Internal("LIST_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, list)
}

func (h *ListHandler) GetAllInFolder(c *gin.Context) {
	folderID, err := strconv.ParseUint(c.Param("folder_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_FOLDER_ID", "invalid folder id", err))
		return
	}

	lists, err := h.service.GetListsByFolder(uint(folderID))
	if err != nil {
		log.Println(err)
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

	var req dto.UpdateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	list, err := h.service.UpdateList(uint(listID), req.Name)
	if err != nil {
		log.Println(err)
		response.HandleError(c, httperrors.Internal("LIST_UPDATE_FAILED", "internal server error", err))
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

	if err := h.service.DeleteList(uint(listID)); err != nil {
		log.Println(err)
		response.HandleError(c, httperrors.Internal("LIST_DELETE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "list deleted"})
}
