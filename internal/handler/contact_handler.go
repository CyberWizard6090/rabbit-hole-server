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
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

const minSearchQueryLen = 3

type ContactHandler struct {
	service service.ContactService
}

func NewContactHandler(service service.ContactService) *ContactHandler {
	return &ContactHandler{service: service}
}

func pageCount(total int64, limit int) int {
	return (int(total) + limit - 1) / limit
}

func (h *ContactHandler) List(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	params, err := pagination.ParseOffset(c.Query("page"), c.Query("limit"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", err.Error(), err))
		return
	}

	contacts, total, err := h.service.ListContacts(c.Request.Context(), uid, params.Limit, params.Offset)
	if err != nil {
		response.HandleError(c, httperrors.Internal("CONTACTS_FETCH_FAILED", "internal server error", err))
		return
	}

	response.SuccessWithPagination(c, http.StatusOK, contacts, &response.Pagination{
		Total: int(total), Page: params.Page, Limit: params.Limit, Pages: pageCount(total, params.Limit),
	})
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

	switch err := h.service.RequestContact(c.Request.Context(), uid, req.ContactID); {
	case err == nil:
	case errors.Is(err, domain.ErrCannotAddSelf):
		response.HandleError(c, httperrors.BadRequest("CANNOT_ADD_SELF", "you cannot add yourself as a contact", err))
		return
	case errors.Is(err, domain.ErrContactRequestLimit):
		response.HandleError(c, httperrors.TooManyRequests("CONTACT_REQUEST_LIMIT_REACHED",
			"too many pending contact requests", err).WithParams(map[string]any{"limit": domain.MaxPendingContactRequests}))
		return
	default:
		response.HandleError(c, httperrors.Internal("CONTACT_REQUEST_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusAccepted, gin.H{"message": "contact request sent"})
}

func (h *ContactHandler) listRequests(c *gin.Context, incoming bool) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	params, err := pagination.ParseOffset(c.Query("page"), c.Query("limit"))
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_PAGINATION", err.Error(), err))
		return
	}

	requests, total, err := h.service.ListRequests(c.Request.Context(), uid, incoming, params.Limit, params.Offset)
	if err != nil {
		response.HandleError(c, httperrors.Internal("CONTACT_REQUESTS_FETCH_FAILED", "internal server error", err))
		return
	}

	response.SuccessWithPagination(c, http.StatusOK, requests, &response.Pagination{
		Total: int(total), Page: params.Page, Limit: params.Limit, Pages: pageCount(total, params.Limit),
	})
}

func (h *ContactHandler) ListIncoming(c *gin.Context) {
	h.listRequests(c, true)
}

func (h *ContactHandler) ListOutgoing(c *gin.Context) {
	h.listRequests(c, false)
}

func (h *ContactHandler) respond(c *gin.Context, param, failureCode, message string,
	action func(c *gin.Context, uid, otherID uint) error,
) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	otherID, err := strconv.ParseUint(c.Param(param), 10, 64)
	if err != nil || otherID == 0 {
		response.HandleError(c, httperrors.BadRequest("INVALID_USER_ID", "invalid user id", err))
		return
	}

	if err := action(c, uid, uint(otherID)); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.HandleError(c, httperrors.NotFound("CONTACT_REQUEST_NOT_FOUND", "contact request not found", err))
			return
		}
		response.HandleError(c, httperrors.Internal(failureCode, "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": message})
}

func (h *ContactHandler) Accept(c *gin.Context) {
	h.respond(c, "user_id", "CONTACT_REQUEST_ACCEPT_FAILED", "contact request accepted",
		func(c *gin.Context, uid, otherID uint) error {
			return h.service.AcceptRequest(c.Request.Context(), uid, otherID)
		})
}

func (h *ContactHandler) Decline(c *gin.Context) {
	h.respond(c, "user_id", "CONTACT_REQUEST_DECLINE_FAILED", "contact request declined",
		func(c *gin.Context, uid, otherID uint) error {
			return h.service.DeclineRequest(c.Request.Context(), uid, otherID)
		})
}

func (h *ContactHandler) Cancel(c *gin.Context) {
	h.respond(c, "user_id", "CONTACT_REQUEST_CANCEL_FAILED", "contact request canceled",
		func(c *gin.Context, uid, otherID uint) error {
			return h.service.CancelRequest(c.Request.Context(), uid, otherID)
		})
}

func (h *ContactHandler) Delete(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	contactID, err := strconv.ParseUint(c.Param("contact_id"), 10, 64)
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("INVALID_CONTACT_ID", "invalid contact id", err))
		return
	}

	if err := h.service.RemoveContact(c.Request.Context(), uid, uint(contactID)); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			response.HandleError(c, httperrors.NotFound("CONTACT_NOT_FOUND", "contact not found", err))
			return
		}
		response.HandleError(c, httperrors.Internal("CONTACT_DELETE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, gin.H{"message": "contact deleted successfully"})
}

func (h *ContactHandler) Search(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	query := strings.TrimSpace(c.Query("q"))
	if len([]rune(query)) < minSearchQueryLen {
		response.HandleError(c, httperrors.BadRequest("QUERY_TOO_SHORT", "query param 'q' must contain at least 3 characters", nil))
		return
	}

	users, err := h.service.SearchUsers(c.Request.Context(), uid, query)
	if err != nil {
		response.HandleError(c, httperrors.Internal("USER_SEARCH_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, users)
}
