package response

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"

	"rabbit-hole-server/internal/domain"
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

	HandleError(context, httperrors.Internal("FETCH_FAILED", "fetch failed", domain.ErrNotFound))

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

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func TestHandleErrorLogsServerErrorsAtErrorAndClientErrorsAtDebug(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cases := []struct {
		name      string
		err       error
		wantLevel string
	}{
		{"unknown error is a 500", errors.New("db exploded"), "ERROR"},
		{"internal app error is a 500", httperrors.Internal("X", "x", errors.New("cause")), "ERROR"},
		{"validation-style 400", httperrors.BadRequest("BAD", "bad", errors.New("cause")), "DEBUG"},
		{"missing row is a 404", domain.ErrNotFound, "DEBUG"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := captureLogs(t)
			ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ginCtx.Set("request_id", "req-1234567")

			HandleError(ginCtx, tc.err)

			var line map[string]any
			if err := json.Unmarshal(buf.Bytes(), &line); err != nil {
				t.Fatalf("no JSON log line: %q", buf.String())
			}
			if line["level"] != tc.wantLevel || line["request_id"] != "req-1234567" {
				t.Fatalf("log = %v, want level %s with the request ID", line, tc.wantLevel)
			}
		})
	}
}

func TestHandleErrorWithNilErrorLogsNothing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := captureLogs(t)
	ginCtx, _ := gin.CreateTestContext(httptest.NewRecorder())
	HandleError(ginCtx, nil)
	if buf.Len() != 0 {
		t.Fatalf("unexpected log output: %s", buf.String())
	}
}
