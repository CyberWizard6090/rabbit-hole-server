package handler

import (
	"errors"
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type TagHandler struct {
	service service.TagService
}

const invalidSpaceIDMessage = "invalid space id"

func NewTagHandler(s service.TagService) *TagHandler {
	return &TagHandler{service: s}
}

func (h *TagHandler) Create(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidSpaceIDMessage)
		return
	}

	var req dto.CreateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	tag, err := h.service.CreateTag(uint(spaceID), service.CreateTagParams{Name: req.Name, Color: req.Color})
	if err != nil {
		if errors.Is(err, domain.ErrTagNameTaken) {
			response.BadRequest(c, "tag with this name already exists")
			return
		}
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusCreated, tag)
}

func (h *TagHandler) GetAll(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidSpaceIDMessage)
		return
	}

	tags, err := h.service.GetAllBySpace(uint(spaceID))
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, tags)
}

func (h *TagHandler) Update(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidSpaceIDMessage)
		return
	}
	tagID, err := strconv.ParseUint(c.Param("tag_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid tag id")
		return
	}

	var req dto.UpdateTagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	tag, err := h.service.UpdateTag(uint(spaceID), uint(tagID), service.UpdateTagParams{Name: req.Name, Color: req.Color})
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, tag)
}

func (h *TagHandler) Delete(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidSpaceIDMessage)
		return
	}
	tagID, err := strconv.ParseUint(c.Param("tag_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid tag id")
		return
	}

	if err := h.service.DeleteTag(uint(spaceID), uint(tagID)); err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "tag deleted"})
}

func (h *TagHandler) Merge(c *gin.Context) {
	spaceID, err := strconv.ParseUint(c.Param("space_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidSpaceIDMessage)
		return
	}

	var req dto.MergeTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := h.service.MergeTags(uint(spaceID), req.SourceTagID, req.TargetTagID); err != nil {
		if errors.Is(err, service.ErrCannotMergeSame) {
			response.BadRequest(c, err.Error())
			return
		}
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "tags merged"})
}
