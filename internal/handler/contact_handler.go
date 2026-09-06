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

type ContactHandler struct {
	service service.ContactService
}

func NewContactHandler(service service.ContactService) *ContactHandler {
	return &ContactHandler{service: service}
}

func (h *ContactHandler) List(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	contacts, err := h.service.GetContacts(uid)
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, contacts)
}

func (h *ContactHandler) Add(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	var req dto.AddContactRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, err.Error())
		return
	}

	if err := h.service.AddContact(uid, req.ContactID); err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, gin.H{
		"message": "contact added successfully",
	})
}

func (h *ContactHandler) Delete(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	contactID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "invalid contact id")
		return
	}

	if err := h.service.DeleteContact(uid, uint(contactID)); err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, gin.H{
		"message": "contact deleted successfully",
	})
}

func (h *ContactHandler) Search(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	query := c.Query("q")
	if query == "" {
		response.BadRequest(c, "query param 'q' is required")
		return
	}

	users, err := h.service.SearchUsers(uid, query)
	if err != nil {
		log.Println(err)
		response.Internal(c)
		return
	}

	response.Success(c, http.StatusOK, users)
}
