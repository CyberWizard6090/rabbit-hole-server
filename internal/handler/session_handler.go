package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

// SessionHandler lets users see the devices they are signed in on and end them.
type SessionHandler struct {
	auth *AuthHandler
}

func NewSessionHandler(auth *AuthHandler) *SessionHandler { return &SessionHandler{auth: auth} }

func (h *SessionHandler) List(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	sessions, err := h.auth.authService.ListSessions(c.Request.Context(), uid, contextutil.GetSessionID(c))
	if err != nil {
		response.HandleError(c, httperrors.Internal("SESSION_LIST_FAILED", "failed to list sessions", err))
		return
	}
	response.Success(c, http.StatusOK, sessions)
}

func (h *SessionHandler) Revoke(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	id := c.Param("session")
	err = h.auth.authService.RevokeSession(c.Request.Context(), uid, id)
	switch {
	case errors.Is(err, service.ErrSessionNotFound):
		response.HandleError(c, httperrors.NotFound("SESSION_NOT_FOUND", "session not found", err))
		return
	case err != nil:
		response.HandleError(c, httperrors.Internal("SESSION_REVOKE_FAILED", "failed to end the session", err))
		return
	}
	if id == contextutil.GetSessionID(c) {
		h.auth.setRefreshCookie(c, "", -1)
	}
	response.Success(c, http.StatusOK, gin.H{"message": "session ended"})
}

func (h *SessionHandler) RevokeOthers(c *gin.Context) {
	uid, err := contextutil.GetUserID(c)
	if err != nil {
		response.Unauthorized(c)
		return
	}
	ended, err := h.auth.authService.RevokeOtherSessions(c.Request.Context(), uid, contextutil.GetSessionID(c))
	if err != nil {
		response.HandleError(c, httperrors.Internal("SESSION_REVOKE_FAILED", "failed to end the sessions", err))
		return
	}
	response.Success(c, http.StatusOK, gin.H{"ended": ended})
}
