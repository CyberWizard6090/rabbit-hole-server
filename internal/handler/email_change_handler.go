package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type EmailChangeHandler struct {
	service *service.EmailChangeService
}

func NewEmailChangeHandler(s *service.EmailChangeService) *EmailChangeHandler {
	return &EmailChangeHandler{service: s}
}

func (h *EmailChangeHandler) Request(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	var req dto.ChangeEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	ctx := service.WithLocale(c.Request.Context(), c.GetHeader("Accept-Language"))
	err = h.service.Request(ctx, uid, req.NewEmail, req.Password)
	switch {
	case passwordBusy(c, err):
	case errors.Is(err, service.ErrInvalidCredentials):
		response.HandleError(c, httperrors.Unauthorized("INVALID_CREDENTIALS", "password is incorrect", err))
	case errors.Is(err, service.ErrSameEmail):
		response.HandleError(c, httperrors.BadRequest("SAME_EMAIL", err.Error(), err))
	case errors.Is(err, domain.ErrEmailTaken):
		response.HandleError(c, httperrors.Conflict("EMAIL_TAKEN", "email is already registered", err))
	case errors.Is(err, service.ErrEmailChangeCooldown):
		response.HandleError(c, httperrors.TooManyRequests("EMAIL_CHANGE_COOLDOWN", err.Error(), err))
	case err != nil:
		response.HandleError(c, httperrors.Internal("EMAIL_CHANGE_FAILED", "internal server error", err))
	default:
		response.Success(c, http.StatusAccepted, gin.H{"message": "a confirmation link was sent to the new address"})
	}
}

func (h *EmailChangeHandler) Confirm(c *gin.Context) {
	var req dto.ConfirmEmailChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	ctx := service.WithLocale(c.Request.Context(), c.GetHeader("Accept-Language"))
	err := h.service.Confirm(ctx, req.Token)
	switch {
	case errors.Is(err, domain.ErrInvalidToken):
		response.HandleError(c, httperrors.BadRequest("INVALID_TOKEN", err.Error(), err))
	case errors.Is(err, domain.ErrEmailTaken):
		response.HandleError(c, httperrors.Conflict("EMAIL_TAKEN", "email is already registered", err))
	case err != nil:
		response.HandleError(c, httperrors.Internal("EMAIL_CHANGE_FAILED", "internal server error", err))
	default:
		response.Success(c, http.StatusOK, gin.H{"message": "email changed, sign in with the new address"})
	}
}

func (h *EmailChangeHandler) Cancel(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	if err := h.service.Cancel(c.Request.Context(), uid); err != nil {
		response.HandleError(c, httperrors.Internal("EMAIL_CHANGE_FAILED", "internal server error", err))
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "pending email change canceled"})
}
