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
