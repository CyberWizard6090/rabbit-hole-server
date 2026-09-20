package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type FolderHandler struct {
	service service.FolderService
}

func NewFolderHandler(s service.FolderService) *FolderHandler {
	return &FolderHandler{service: s}
}

func (h *FolderHandler) Create(c *gin.Context) {
	if _, err := contextutil.GetUserID(c); err != nil {
		response.Unauthorized(c)
		return
	}

	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_SPACE_ID", "invalid space id", err))
		return
	}

	var req dto.CreateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	folder, err := h.service.CreateFolder(uint(spaceID), req.Name)
	if err != nil {
		response.HandleError(c, httperrors.Internal("FOLDER_CREATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, folder)
}

func (h *FolderHandler) GetAll(c *gin.Context) {
	if _, err := contextutil.GetUserID(c); err != nil {
		response.Unauthorized(c)
		return
	}

	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_SPACE_ID", "invalid space id", err))
		return
	}

	folders, err := h.service.GetAllFolders(uint(spaceID))
	if err != nil {
		response.HandleError(c, httperrors.Internal("FOLDERS_FETCH_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, folders)
}

func (h *FolderHandler) Update(c *gin.Context) {
	folderID, err := strconv.ParseUint(c.Param("folder_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_FOLDER_ID", "invalid folder id", err))
		return
	}

	var req dto.UpdateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	folder, err := h.service.UpdateFolder(uint(folderID), req.Name)
	if err != nil {
		response.HandleError(c, httperrors.Internal("FOLDER_UPDATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, folder)
}

func (h *FolderHandler) Delete(c *gin.Context) {
	folderID, err := strconv.ParseUint(c.Param("folder_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_FOLDER_ID", "invalid folder id", err))
		return
	}

	if err := h.service.DeleteFolder(uint(folderID)); err != nil {
		response.HandleError(c, httperrors.Internal("FOLDER_DELETE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "folder deleted"})
}
