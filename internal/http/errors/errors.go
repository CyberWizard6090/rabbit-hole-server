package errors

import (
	"fmt"
	"net/http"
)

type AppError struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Status  int               `json:"-"`
	Fields  map[string]string `json:"fields,omitempty"`
	Err     error             `json:"-"`
}

func (e *AppError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func New(code, message string, status int) *AppError {
	return &AppError{Code: code, Message: message, Status: status}
}

func BadRequest(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusBadRequest, Err: err}
}

func Unauthorized(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusUnauthorized, Err: err}
}

func Forbidden(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusForbidden, Err: err}
}

func NotFound(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusNotFound, Err: err}
}

func Conflict(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusConflict, Err: err}
}

func UnprocessableEntity(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusUnprocessableEntity, Err: err}
}

func TooManyRequests(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusTooManyRequests, Err: err}
}

func Internal(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusInternalServerError, Err: err}
}

func ServiceUnavailable(code, message string, err error) *AppError {
	return &AppError{Code: code, Message: message, Status: http.StatusServiceUnavailable, Err: err}
}

func ValidationFailed(fields map[string]string, err error) *AppError {
	return &AppError{
		Code:    "VALIDATION_FAILED",
		Message: "invalid request",
		Status:  http.StatusBadRequest,
		Fields:  fields,
		Err:     err,
	}
}

func Wrap(code, message string, status int, err error) *AppError {
	if message == "" {
		message = err.Error()
	}
	return &AppError{Code: code, Message: message, Status: status, Err: err}
}

func Wrapf(code, message string, status int, err error, format string, args ...any) *AppError {
	if err == nil {
		return New(code, fmt.Sprintf(message, args...), status)
	}
	return &AppError{Code: code, Message: fmt.Sprintf(message, args...), Status: status, Err: err}
}
