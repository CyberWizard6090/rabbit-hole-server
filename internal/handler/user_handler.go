package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type PendingEmailReader interface {
	Pending(ctx context.Context, userID uint) string
}

type UserHandler struct {
	service service.UserService
	pending PendingEmailReader
}

func (h *UserHandler) WithPendingEmail(reader PendingEmailReader) *UserHandler {
	h.pending = reader
	return h
}

func NewUserHandler(service service.UserService) *UserHandler {
	return &UserHandler{service: service}
}

func (h *UserHandler) GetCurrentUser(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	user, err := h.service.GetUserByID(c.Request.Context(), uid)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.HandleError(c, httperrors.NotFound("USER_NOT_FOUND", "user not found", err))
			return
		}
		response.HandleError(c, httperrors.Internal("USER_FETCH_FAILED", "internal server error", err))
		return
	}

	if h.pending != nil {
		user.PendingEmail = h.pending.Pending(c.Request.Context(), uid)
	}
	response.Success(c, http.StatusOK, user)
}

func (h *UserHandler) UpdateProfile(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	var req dto.UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	user, err := h.service.UpdateProfile(c.Request.Context(), uid, service.UpdateProfileParams{
		Username:  req.Username,
		FirstName: req.FirstName,
		LastName:  req.LastName,
		Bio:       req.Bio,
		TimeZone:  req.TimeZone,
	})
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.HandleError(c, httperrors.NotFound("USER_NOT_FOUND", "user not found", err))
			return
		}
		response.HandleError(c, httperrors.Internal("PROFILE_UPDATE_FAILED", "internal server error", err))
		return
	}

	response.Success(c, http.StatusOK, user)
}
