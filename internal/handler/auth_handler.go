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

type AuthHandler struct {
	authService  *service.AuthService
	secureCookie bool
}

const authCookiePath = "/api/v1/auth"

func NewAuthHandler(s *service.AuthService, secureCookie bool) *AuthHandler {
	return &AuthHandler{authService: s, secureCookie: secureCookie}
}

func (h *AuthHandler) refreshCookieMaxAge() int {
	return int(h.authService.RefreshTTL().Seconds())
}

func (h *AuthHandler) setRefreshCookie(c *gin.Context, value string, maxAge int) {
	c.Header("Set-Cookie", (&http.Cookie{ //nolint:gosec // Secure is configurable: local dev runs over plain HTTP
		Name:     "refresh_token",
		Value:    value,
		MaxAge:   maxAge,
		Path:     authCookiePath,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteLaxMode,
	}).String())
}

type registerInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=8,max=128"`
	Username string `json:"username" binding:"omitempty,min=3"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var input registerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ValidationError(c, err)
		return
	}

	ctx := service.WithLocale(c.Request.Context(), c.GetHeader("Accept-Language"))
	if err := h.authService.Register(ctx, input.Email, input.Password, input.Username); err != nil {
		if errors.Is(err, domain.ErrEmailTaken) {
			response.HandleError(c, httperrors.Conflict("EMAIL_TAKEN", "email is already registered", err))
			return
		}
		if errors.Is(err, domain.ErrUsernameTaken) {
			response.HandleError(c, httperrors.Conflict("USERNAME_TAKEN", "username is already registered", err))
			return
		}
		if errors.Is(err, service.ErrWeakPassword) {
			response.HandleError(c, httperrors.BadRequest("WEAK_PASSWORD", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Internal("REGISTRATION_FAILED", "failed to register user", err))
		return
	}

	response.Success(c, http.StatusCreated, gin.H{"message": "registration successful"})
}

type loginInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

func (h *AuthHandler) Login(c *gin.Context) {
	var input loginInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ValidationError(c, err)
		return
	}

	tokenPair, err := h.authService.Login(c.Request.Context(), input.Email, input.Password)
	if err != nil {
		if errors.Is(err, service.ErrEmailNotVerified) {
			response.HandleError(c, httperrors.Forbidden("EMAIL_NOT_VERIFIED", err.Error(), err))
			return
		}
		response.HandleError(c, httperrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password", err))
		return
	}

	h.setRefreshCookie(c, tokenPair.RefreshToken, h.refreshCookieMaxAge())

	response.Success(c, http.StatusOK, gin.H{"access_token": tokenPair.AccessToken})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil {
		response.HandleError(c, httperrors.Unauthorized("TOKEN_MISSING", "refresh token missing", err))
		return
	}

	tokenPair, err := h.authService.Refresh(c.Request.Context(), refreshToken)
	if err != nil {
		response.HandleError(c, httperrors.Unauthorized("TOKEN_EXPIRED", "invalid or expired refresh token", err))
		return
	}

	h.setRefreshCookie(c, tokenPair.RefreshToken, h.refreshCookieMaxAge())

	response.Success(c, http.StatusOK, gin.H{"access_token": tokenPair.AccessToken})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("ALREADY_LOGGED_OUT", "already logged out", err))
		return
	}

	if err := h.authService.Logout(c.Request.Context(), refreshToken); err != nil {
		response.HandleError(c, httperrors.Internal("LOGOUT_FAILED", "failed to logout", err))
		return
	}

	h.setRefreshCookie(c, "", -1)

	response.Success(c, http.StatusOK, gin.H{"message": "logged out successfully"})
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	var req dto.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	tokenPair, err := h.authService.ChangePassword(c.Request.Context(), uid, req.CurrentPassword, req.NewPassword)
	switch {
	case errors.Is(err, service.ErrWeakPassword):
		response.HandleError(c, httperrors.BadRequest("WEAK_PASSWORD", err.Error(), err))
		return
	case errors.Is(err, service.ErrInvalidCredentials):
		response.HandleError(c, httperrors.Unauthorized("INVALID_CREDENTIALS", "current password is incorrect", err))
		return
	case err != nil:
		response.HandleError(c, httperrors.Internal("PASSWORD_CHANGE_FAILED", "internal server error", err))
		return
	}

	h.setRefreshCookie(c, tokenPair.RefreshToken, h.refreshCookieMaxAge())
	response.Success(c, http.StatusOK, gin.H{"access_token": tokenPair.AccessToken})
}

func (h *AuthHandler) LogoutAll(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	if err := h.authService.LogoutAll(c.Request.Context(), uid); err != nil {
		response.HandleError(c, httperrors.Internal("LOGOUT_FAILED", "failed to logout", err))
		return
	}
	h.setRefreshCookie(c, "", -1)
	response.Success(c, http.StatusOK, gin.H{"message": "logged out everywhere"})
}

func (h *AuthHandler) DeleteAccount(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}

	var req dto.DeleteAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.ValidationError(c, err)
		return
	}

	err = h.authService.DeleteAccount(c.Request.Context(), uid, req.Password)
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):
		response.HandleError(c, httperrors.Unauthorized("INVALID_CREDENTIALS", "password is incorrect", err))
		return
	case errors.Is(err, service.ErrOwnsWorkspaces):
		response.HandleError(c, httperrors.Conflict("OWNS_WORKSPACES", err.Error(), err))
		return
	case err != nil:
		response.HandleError(c, httperrors.Internal("ACCOUNT_DELETE_FAILED", "internal server error", err))
		return
	}

	h.setRefreshCookie(c, "", -1)
	response.Success(c, http.StatusOK, gin.H{"message": "account deleted"})
}
