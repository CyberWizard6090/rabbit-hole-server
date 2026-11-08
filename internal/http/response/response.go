package response

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

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

// ErrorPayload is the error body of every failed response. It carries no
// human readable text: the client translates Code and fills the translation
// with Params. Details lists per-field problems of an invalid request.
// RequestID matches the X-Request-ID header and the server log line.
type ErrorPayload struct {
	Code      string              `json:"code"`
	Params    map[string]any      `json:"params,omitempty"`
	Details   []httperrors.Detail `json:"details,omitempty"`
	RequestID string              `json:"request_id,omitempty"`
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

func write(c *gin.Context, status int, code string, params map[string]any, details []httperrors.Detail) {
	c.JSON(status, APIResponse{
		Error: &ErrorPayload{
			Code:      code,
			Params:    params,
			Details:   details,
			RequestID: contextutil.GetRequestID(c),
		},
	})
}

// Fail answers with an error that has only a code and optional params.
func Fail(c *gin.Context, status int, code string, params map[string]any) {
	write(c, status, code, params, nil)
}

func HandleError(c *gin.Context, err error) {
	status, code, params, details := classify(err)
	logHandled(c, status, code, err)
	write(c, status, code, params, details)
}

func classify(err error) (status int, code string, params map[string]any, details []httperrors.Detail) {
	if errors.Is(err, domain.ErrNotFound) {
		return http.StatusNotFound, "NOT_FOUND", nil, nil
	}
	var appErr *httperrors.AppError
	if errors.As(err, &appErr) {
		return appErr.Status, appErr.Code, appErr.Params, appErr.Details
	}
	return http.StatusInternalServerError, "INTERNAL_ERROR", nil, nil
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

// ValidationError answers 400 for a request body or query that could not be
// bound: every broken field becomes an entry of details.
func ValidationError(c *gin.Context, err error) {
	if err == nil {
		Fail(c, http.StatusBadRequest, "VALIDATION_FAILED", nil)
		return
	}

	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		Fail(c, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", map[string]any{"max_bytes": tooLarge.Limit})
		return
	}

	var syntax *json.SyntaxError
	if errors.As(err, &syntax) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		Fail(c, http.StatusBadRequest, "INVALID_JSON", nil)
		return
	}

	write(c, http.StatusBadRequest, "VALIDATION_FAILED", nil, validationDetails(err))
}

// InvalidID answers 400 for a path parameter that is not a valid identifier.
func InvalidID(c *gin.Context, param string) {
	Fail(c, http.StatusBadRequest, "INVALID_ID", map[string]any{"param": param})
}

func Unauthorized(c *gin.Context) {
	Fail(c, http.StatusUnauthorized, "UNAUTHORIZED", nil)
}

func Forbidden(c *gin.Context) {
	Fail(c, http.StatusForbidden, "FORBIDDEN", nil)
}

func Internal(c *gin.Context) {
	Fail(c, http.StatusInternalServerError, "INTERNAL_ERROR", nil)
}

func validationDetails(err error) []httperrors.Detail {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := typeErr.Field
		if field == "" {
			field = "request"
		}
		return []httperrors.Detail{{
			Field:  field,
			Code:   "INVALID_TYPE",
			Params: map[string]any{"expected": jsonTypeName(typeErr)},
		}}
	}

	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return nil
	}
	details := make([]httperrors.Detail, 0, len(validationErrs))
	for _, fe := range validationErrs {
		details = append(details, fieldDetail(fe))
	}
	return details
}
