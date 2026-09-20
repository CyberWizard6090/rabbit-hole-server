package response

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
	"gorm.io/gorm"

	httperrors "rabbit-hole-server/internal/http/errors"
)

func TestHandleErrorRendersAllSupportedStatuses(t *testing.T) {
	cases := []struct {
		name   string
		status int
		code   string
		err    error
	}{
		{name: "bad request", status: http.StatusBadRequest, code: "BAD_INPUT", err: httperrors.BadRequest("BAD_INPUT", "bad input", errors.New("input"))},
		{name: "unauthorized", status: http.StatusUnauthorized, code: "AUTH_REQUIRED", err: httperrors.Unauthorized("AUTH_REQUIRED", "authentication required", nil)},
		{name: "forbidden", status: http.StatusForbidden, code: "ACCESS_DENIED", err: httperrors.Forbidden("ACCESS_DENIED", "access denied", nil)},
		{name: "not found", status: http.StatusNotFound, code: "RESOURCE_NOT_FOUND", err: httperrors.NotFound("RESOURCE_NOT_FOUND", "resource not found", nil)},
		{name: "conflict", status: http.StatusConflict, code: "RESOURCE_CONFLICT", err: httperrors.Conflict("RESOURCE_CONFLICT", "resource conflict", nil)},
		{name: "unprocessable entity", status: http.StatusUnprocessableEntity, code: "DOMAIN_INVALID", err: httperrors.UnprocessableEntity("DOMAIN_INVALID", "domain rule failed", nil)},
		{name: "too many requests", status: http.StatusTooManyRequests, code: "RATE_LIMITED", err: httperrors.TooManyRequests("RATE_LIMITED", "too many requests", nil)},
		{name: "internal server error", status: http.StatusInternalServerError, code: "INTERNAL_FAILURE", err: httperrors.Internal("INTERNAL_FAILURE", "internal failure", nil)},
		{name: "service unavailable", status: http.StatusServiceUnavailable, code: "SERVICE_DOWN", err: httperrors.ServiceUnavailable("SERVICE_DOWN", "service unavailable", nil)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			recorder := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(recorder)

			HandleError(context, tc.err)

			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, tc.status, recorder.Body.String())
			}

			var body APIResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v; body=%s", err, recorder.Body.String())
			}
			if body.Error == nil {
				t.Fatal("response.error is nil")
			}
			if body.Error.Code != tc.code {
				t.Fatalf("error.code = %q, want %q", body.Error.Code, tc.code)
			}
			if body.Error.Message == "" {
				t.Fatal("error.message is empty")
			}
		})
	}
}

func TestValidationErrorRendersFieldErrors(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	type request struct {
		Email    string `validate:"required,email"`
		Password string `validate:"min=8"`
	}
	validationErr := validator.New().Struct(request{Password: "short"})
	ValidationError(context, validationErr)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var body APIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error == nil || body.Error.Code != "VALIDATION_FAILED" {
		t.Fatalf("unexpected error payload: %+v", body.Error)
	}
	if body.Error.Fields["Email"] != "required" || body.Error.Fields["Password"] != "min" {
		t.Fatalf("unexpected validation fields: %+v", body.Error.Fields)
	}
}

func TestHandleErrorHidesUnknownInternalError(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	HandleError(context, errors.New("database password leaked"))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if recorder.Body.String() == "" || strings.Contains(recorder.Body.String(), "database password leaked") {
		t.Fatalf("internal error leaked in response: %s", recorder.Body.String())
	}
}

func TestHandleErrorClassifiesRecordNotFound(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)

	HandleError(context, httperrors.Internal("FETCH_FAILED", "fetch failed", gorm.ErrRecordNotFound))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	var body APIResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error == nil || body.Error.Code != "NOT_FOUND" {
		t.Fatalf("unexpected error payload: %+v", body.Error)
	}
}
