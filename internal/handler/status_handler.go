package handler

import (
	"log"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type StatusHandler struct {
	service service.StatusService
}

const invalidListIDMessage = "invalid list id"

func NewStatusHandler(
	service service.StatusService,
) *StatusHandler {

	return &StatusHandler{
		service: service,
	}
}

func (h *StatusHandler) Create(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidListIDMessage)
		return
	}

	var req dto.CreateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	status, err := h.service.Create(service.CreateStatusParams{
		ListID:   uint(listID),
		Name:     req.Name,
		Color:    req.Color,
		Position: req.Position,
		Type:     req.Type,
	})
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusCreated, status)
}

func (h *StatusHandler) GetAll(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidListIDMessage)
		return
	}

	statuses, err := h.service.GetAllByList(uint(listID))
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, statuses)
}

func (h *StatusHandler) Update(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidListIDMessage)
		return
	}
	statusID, err := strconv.ParseUint(c.Param("status_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid status id")
		return
	}

	var req dto.UpdateStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	status, err := h.service.Update(uint(listID), uint(statusID), service.UpdateStatusParams{
		Name:  req.Name,
		Color: req.Color,
		Type:  req.Type,
	})
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, status)
}

func (h *StatusHandler) Delete(c *gin.Context) {
	listID, err := strconv.ParseUint(c.Param("list_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, invalidListIDMessage)
		return
	}
	statusID, err := strconv.ParseUint(c.Param("status_id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid status id")
		return
	}

	if err := h.service.Delete(uint(listID), uint(statusID)); err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "status deleted"})
}
