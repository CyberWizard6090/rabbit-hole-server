package response

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type Pagination struct {
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Pages int `json:"pages"`
}

type APIResponse struct {
	Data       interface{} `json:"data,omitempty"`
	Error      string      `json:"error,omitempty"`
	Pagination *Pagination `json:"pagination,omitempty"`
}

func Success(c *gin.Context, status int, data interface{}) {
	c.JSON(status, APIResponse{
		Data: data,
	})
}

func SuccessWithPagination(
	c *gin.Context,
	status int,
	data interface{},
	pagination *Pagination,
) {
	c.JSON(status, APIResponse{
		Data:       data,
		Pagination: pagination,
	})
}

func Error(c *gin.Context, status int, message string) {
	c.JSON(status, APIResponse{
		Error: message,
	})
}

func BadRequest(c *gin.Context, message string) {
	Error(c, http.StatusBadRequest, message)
}

func Unauthorized(c *gin.Context) {
	Error(c, http.StatusUnauthorized, "unauthorized")
}

func NotFound(c *gin.Context, message string) {
	Error(c, http.StatusNotFound, message)
}

func Internal(c *gin.Context) {
	Error(c, http.StatusInternalServerError, "internal server error")
}
