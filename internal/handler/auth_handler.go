package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type AuthHandler struct {
	authService *service.AuthService
}

const authCookiePath = "/api/v1/auth"

func NewAuthHandler(s *service.AuthService) *AuthHandler {
	return &AuthHandler{authService: s}
}

func (h *AuthHandler) refreshCookieMaxAge() int {
	return int(h.authService.RefreshTTL().Seconds())
}

type registerInput struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
	Username string `json:"username" binding:"omitempty,min=3"`
}

func (h *AuthHandler) Register(c *gin.Context) {
	var input registerInput
	if err := c.ShouldBindJSON(&input); err != nil {
		response.ValidationError(c, err)
		return
	}

	if err := h.authService.Register(input.Email, input.Password, input.Username); err != nil {
		if errors.Is(err, domain.ErrEmailTaken) {
			response.HandleError(c, httperrors.Conflict("EMAIL_TAKEN", "email is already registered", err))
			return
		}
		if errors.Is(err, domain.ErrUsernameTaken) {
			response.HandleError(c, httperrors.Conflict("USERNAME_TAKEN", "username is already registered", err))
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

	tokenPair, err := h.authService.Login(input.Email, input.Password)
	if err != nil {
		response.HandleError(c, httperrors.Unauthorized("INVALID_CREDENTIALS", "invalid email or password", err))
		return
	}

	c.SetCookie("refresh_token", tokenPair.RefreshToken, h.refreshCookieMaxAge(), authCookiePath, "", false, true)

	response.Success(c, http.StatusOK, gin.H{"access_token": tokenPair.AccessToken})
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil {
		response.HandleError(c, httperrors.Unauthorized("TOKEN_MISSING", "refresh token missing", err))
		return
	}

	tokenPair, err := h.authService.Refresh(refreshToken)
	if err != nil {
		response.HandleError(c, httperrors.Unauthorized("TOKEN_EXPIRED", "invalid or expired refresh token", err))
		return
	}

	c.SetCookie("refresh_token", tokenPair.RefreshToken, h.refreshCookieMaxAge(), authCookiePath, "", false, true)

	response.Success(c, http.StatusOK, gin.H{"access_token": tokenPair.AccessToken})
}

func (h *AuthHandler) Logout(c *gin.Context) {
	refreshToken, err := c.Cookie("refresh_token")
	if err != nil {
		response.HandleError(c, httperrors.BadRequest("ALREADY_LOGGED_OUT", "already logged out", err))
		return
	}

	if err := h.authService.Logout(refreshToken); err != nil {
		response.HandleError(c, httperrors.Internal("LOGOUT_FAILED", "failed to logout", err))
		return
	}

	c.SetCookie("refresh_token", "", -1, authCookiePath, "", false, true)

	response.Success(c, http.StatusOK, gin.H{"message": "logged out successfully"})
}
