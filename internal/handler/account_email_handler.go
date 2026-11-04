package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/dto"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type AccountEmailHandler struct {
	service *service.AccountEmailService
}

func NewAccountEmailHandler(s *service.AccountEmailService) *AccountEmailHandler {
	return &AccountEmailHandler{service: s}
}

const mailSentMessage = "if an account with this email can use this, a message is on its way"

func (h *AccountEmailHandler) VerifyEmail(c *gin.Context) {
	var req dto.VerifyEmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.service.VerifyEmail(c.Request.Context(), req.Token); err != nil {
		if errors.Is(err, domain.ErrInvalidToken) {
			response.HandleError(c, httperrors.BadRequest("INVALID_TOKEN", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Internal("EMAIL_VERIFICATION_FAILED", "internal server error", err))
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "email confirmed"})
}

func (h *AccountEmailHandler) ResendVerification(c *gin.Context) {
	var req dto.EmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	ctx := service.WithLocale(c.Request.Context(), c.GetHeader("Accept-Language"))
	h.service.ResendVerification(ctx, req.Email)
	response.Success(c, http.StatusAccepted, gin.H{"message": mailSentMessage})
}

func (h *AccountEmailHandler) ForgotPassword(c *gin.Context) {
	var req dto.EmailRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	ctx := service.WithLocale(c.Request.Context(), c.GetHeader("Accept-Language"))
	h.service.ForgotPassword(ctx, req.Email)
	response.Success(c, http.StatusAccepted, gin.H{"message": mailSentMessage})
}

func (h *AccountEmailHandler) ResetPassword(c *gin.Context) {
	var req dto.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}
	if err := h.service.ResetPassword(c.Request.Context(), req.Token, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidToken):
			response.HandleError(c, httperrors.BadRequest("INVALID_TOKEN", err.Error(), err))
		case errors.Is(err, service.ErrWeakPassword):
			response.HandleError(c, httperrors.BadRequest("WEAK_PASSWORD", err.Error(), err))
		default:
			response.HandleError(c, httperrors.Internal("PASSWORD_RESET_FAILED", "internal server error", err))
		}
		return
	}
	response.Success(c, http.StatusOK, gin.H{"message": "password changed, sign in with the new password"})
}
