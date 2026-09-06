package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
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
		response.BadRequest(c, "invalid space id")
		return
	}

	var req dto.CreateListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	list, err := h.service.CreateList(uint(spaceID), req.Name, req.ParentStatusID)
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusCreated, list)
}

func (h *ListHandler) GetAllInSpace(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid space id")
		return
	}

	space, err := h.service.GetSpaceByID(uint(spaceID))
	if err != nil {
		log.Println(err)
		response.Internal(c)
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
		response.BadRequest(c, "invalid folder id")
		return
	}

	folder, err := h.folderService.GetFolderByID(uint(folderID))
	if err != nil {
		response.NotFound(c, "folder not found")
		return
	}

	var req dto.CreateListRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	list, err := h.service.CreateListInFolder(folder.SpaceID, uint(folderID), req.Name)
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusCreated, list)
}

func (h *ListHandler) GetAllInFolder(c *gin.Context) {
	folderID, err := strconv.ParseUint(c.Param("folder_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid folder id")
		return
	}

	lists, err := h.service.GetListsByFolder(uint(folderID))
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, lists)
}

func (h *ListHandler) Update(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid list id")
		return
	}

	var req dto.UpdateFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	list, err := h.service.UpdateList(uint(listID), req.Name)
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, list)
}

func (h *ListHandler) Delete(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid list id")
		return
	}

	if err := h.service.DeleteList(uint(listID)); err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "list deleted"})
}
