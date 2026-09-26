package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type UserHandler struct {
	service service.UserService
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

	user, err := h.service.GetUserByID(uid)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.HandleError(c, httperrors.NotFound("USER_NOT_FOUND", "user not found", err))
			return
		}
		response.HandleError(c, httperrors.Internal("USER_FETCH_FAILED", "internal server error", err))
		return
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
		response.BadRequest(c, err.Error())
		return
	}

	user, err := h.service.UpdateProfile(uid, service.UpdateProfileParams{
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
