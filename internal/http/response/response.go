package response

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"rabbit-hole-server/internal/domain"
	contextutil "rabbit-hole-server/internal/http/context"
	httperrors "rabbit-hole-server/internal/http/errors"
)

type Pagination struct {
	Total int `json:"total"`
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Pages int `json:"pages"`
}

type ErrorPayload struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type APIResponse struct {
	Data       interface{}   `json:"data,omitempty"`
	Error      *ErrorPayload `json:"error,omitempty"`
	Pagination *Pagination   `json:"pagination,omitempty"`
}

func Success(c *gin.Context, status int, data interface{}) {
	c.JSON(status, APIResponse{Data: data})
}

func SuccessWithPagination(c *gin.Context, status int, data interface{}, pagination *Pagination) {
	c.JSON(status, APIResponse{Data: data, Pagination: pagination})
}

func ErrorWithCode(c *gin.Context, status int, code, message string, fields map[string]string) {
	c.JSON(status, APIResponse{
		Error: &ErrorPayload{Code: code, Message: message, Fields: fields},
	})
}

func Error(c *gin.Context, status int, message string) {
	ErrorWithCode(c, status, defaultErrorCode(status), message, nil)
}

func HandleError(c *gin.Context, err error) {
	status, code, message, fields := classify(err)
	logHandled(c, status, code, err)
	ErrorWithCode(c, status, code, message, fields)
}

func classify(err error) (status int, code, message string, fields map[string]string) {
	if errors.Is(err, domain.ErrNotFound) {
		return http.StatusNotFound, "NOT_FOUND", "resource not found", nil
	}
	var appErr *httperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Status, appErr.Code, appErr.Message, appErr.Fields
	}
	return http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", nil
}

func logHandled(c *gin.Context, status int, code string, err error) {
	if err == nil {
		return
	}
	level := slog.LevelDebug
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	logger := slog.Default()
	ctx := context.Background()
	if c.Request != nil {
		ctx = c.Request.Context()
	}
	if !logger.Enabled(ctx, level) {
		return
	}
	logger.Log(ctx, level, "request failed",
		"request_id", contextutil.GetRequestID(c),
		"route", c.FullPath(),
		"status", status,
		"code", code,
		"error", err.Error(),
	)
}

func ValidationError(c *gin.Context, err error) {
	if err == nil {
		ErrorWithCode(c, http.StatusBadRequest, "VALIDATION_FAILED", "invalid request", nil)
		return
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		ErrorWithCode(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request body is too large", nil)
		return
	}

	fields := parseValidationFields(err)
	ErrorWithCode(c, http.StatusBadRequest, "VALIDATION_FAILED", "invalid request", fields)
}

func BadRequest(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusBadRequest, "BAD_REQUEST", message, nil)
}

func Unauthorized(c *gin.Context) {
	ErrorWithCode(c, http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized", nil)
}

func Forbidden(c *gin.Context, message string) {
	if message == "" {
		message = "forbidden"
	}
	ErrorWithCode(c, http.StatusForbidden, "FORBIDDEN", message, nil)
}

func NotFound(c *gin.Context, message string) {
	ErrorWithCode(c, http.StatusNotFound, "NOT_FOUND", message, nil)
}

func Internal(c *gin.Context) {
	ErrorWithCode(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error", nil)
}

func defaultErrorCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "BAD_REQUEST"
	case http.StatusUnauthorized:
		return "UNAUTHORIZED"
	case http.StatusForbidden:
		return "FORBIDDEN"
	case http.StatusNotFound:
		return "NOT_FOUND"
	case http.StatusConflict:
		return "CONFLICT"
	case http.StatusUnprocessableEntity:
		return "UNPROCESSABLE_ENTITY"
	case http.StatusTooManyRequests:
		return "RATE_LIMIT_EXCEEDED"
	case http.StatusInternalServerError:
		return "INTERNAL_ERROR"
	case http.StatusServiceUnavailable:
		return "SERVICE_UNAVAILABLE"
	default:
		return "ERROR"
	}
}

func parseValidationFields(err error) map[string]string {
	fields := map[string]string{}
	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return fields
	}

	for _, e := range validationErrs {
		field := strings.TrimSpace(e.Field())
		if field == "" {
			field = "request"
		}
		fields[field] = e.Tag()
	}
	return fields
}
