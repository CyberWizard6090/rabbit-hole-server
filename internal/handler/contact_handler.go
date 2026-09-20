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
		response.HandleError(c, httperrors.Internal("CONTACTS_FETCH_FAILED", "internal server error", err))
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
		response.ValidationError(c, err)
		return
	}

	if err := h.service.AddContact(uid, req.ContactID); err != nil {
		if errors.Is(err, domain.ErrContactAlreadyAdded) {
			response.HandleError(c, httperrors.Conflict("CONTACT_ALREADY_ADDED", "contact already added", err))
			return
		}
		response.HandleError(c, httperrors.Internal("CONTACT_ADD_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusCreated, gin.H{
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
		response.HandleError(c, httperrors.BadRequest("INVALID_CONTACT_ID", "invalid contact id", err))
		return
	}

	if err := h.service.DeleteContact(uid, uint(contactID)); err != nil {
		response.HandleError(c, httperrors.Internal("CONTACT_DELETE_FAILED", "internal server error", err))
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
	if len([]rune(query)) < 2 {
		response.HandleError(c, httperrors.BadRequest("QUERY_TOO_SHORT", "query param 'q' must contain at least 2 characters", nil))
		return
	}

	users, err := h.service.SearchUsers(uid, query)
	if err != nil {
		response.HandleError(c, httperrors.Internal("USER_SEARCH_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, users)
}
