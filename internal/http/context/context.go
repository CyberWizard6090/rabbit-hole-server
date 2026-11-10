package contextutil

import (
	"errors"

	"github.com/gin-gonic/gin"
)

func GetUserID(c *gin.Context) (uint, error) {
	userID, exists := c.Get("userID")
	if !exists {
		return 0, errors.New("user not found")
	}

	uid, ok := userID.(uint)
	if !ok {
		return 0, errors.New("invalid user id")
	}

	return uid, nil
}

const RequestIDKey = "request_id"

func GetRequestID(c *gin.Context) string {
	return c.GetString(RequestIDKey)
}

// SessionIDKey holds the session (refresh token family) the access token of the request belongs to.
const SessionIDKey = "sessionID"

func GetSessionID(c *gin.Context) string {
	return c.GetString(SessionIDKey)
}

const TokenVersionKey = "tokenVersion"

func GetTokenVersion(c *gin.Context) int {
	if version, ok := c.Get(TokenVersionKey); ok {
		if v, ok := version.(int); ok {
			return v
		}
	}
	return 0
}
