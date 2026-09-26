package middleware

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/http/response"
	"rabbit-hole-server/internal/service"
)

type permissionCheckerStub struct {
	allowed bool
	err     error
	calls   int

	calledResource string
	calledUserID   uint
	calledID       uint
	calledCode     string
}

func (s *permissionCheckerStub) HasWorkspacePermission(userID, resourceID uint, code string) (bool, error) {
	s.record(ResourceWorkspace, userID, resourceID, code)
	return s.allowed, s.err
}

func (s *permissionCheckerStub) HasSpacePermission(userID, resourceID uint, code string) (bool, error) {
	s.record(ResourceSpace, userID, resourceID, code)
	return s.allowed, s.err
}

func (s *permissionCheckerStub) HasFolderPermission(userID, resourceID uint, code string) (bool, error) {
	s.record(ResourceFolder, userID, resourceID, code)
	return s.allowed, s.err
}

func (s *permissionCheckerStub) HasListPermission(userID, resourceID uint, code string) (bool, error) {
	s.record(ResourceList, userID, resourceID, code)
	return s.allowed, s.err
}

func (s *permissionCheckerStub) HasTaskPermission(userID, resourceID uint, code string) (bool, error) {
	s.record(ResourceTask, userID, resourceID, code)
	return s.allowed, s.err
}

func (s *permissionCheckerStub) record(resource Resource, userID, resourceID uint, code string) {
	s.calls++
	s.calledResource = string(resource)
	s.calledUserID = userID
	s.calledID = resourceID
	s.calledCode = code
}

func TestRequirePermissionDispatchesToCorrectChecker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name     string
		resource Resource
	}{
		{name: "workspace", resource: ResourceWorkspace},
		{name: "space", resource: ResourceSpace},
		{name: "folder", resource: ResourceFolder},
		{name: "list", resource: ResourceList},
		{name: "task", resource: ResourceTask},
	}

	const userID = uint(42)
	const resourceID = uint(123)
	const permissionCode = "task.read"

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &permissionCheckerStub{allowed: true}
			router := newPermissionRouter(checker, tt.resource, permissionCode, userID)

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, permissionTestPath(resourceID), nil)
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
			}
			if checker.calls != 1 {
				t.Fatalf("checker calls=%d want 1", checker.calls)
			}
			if checker.calledResource != string(tt.resource) {
				t.Fatalf("resource=%q want %q", checker.calledResource, tt.resource)
			}
			if checker.calledUserID != userID || checker.calledID != resourceID || checker.calledCode != permissionCode {
				t.Fatalf("checker args=(%d,%d,%q)", checker.calledUserID, checker.calledID, checker.calledCode)
			}
		})
	}
}

func TestRequirePermissionRejectsMissingUser(t *testing.T) {
	gin.SetMode(gin.TestMode)

	checker := &permissionCheckerStub{allowed: true}
	handlerCalled := false
	router := gin.New()
	router.GET("/resource/:space_id", RequirePermission(checker, ResourceSpace, "space.read"), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/10", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusUnauthorized)
	}
	if checker.calls != 0 {
		t.Fatalf("checker calls=%d want 0", checker.calls)
	}
	if handlerCalled {
		t.Fatal("downstream handler was called")
	}
}

func TestRequirePermissionRejectsInvalidUserIDType(t *testing.T) {
	gin.SetMode(gin.TestMode)

	checker := &permissionCheckerStub{allowed: true}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", uint64(42))
		c.Next()
	})
	handlerCalled := false
	router.GET("/resource/:space_id", RequirePermission(checker, ResourceSpace, "space.read"), func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/10", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusUnauthorized)
	}
	if checker.calls != 0 {
		t.Fatalf("checker calls=%d want 0", checker.calls)
	}
	if handlerCalled {
		t.Fatal("downstream handler was called")
	}
}

func TestRequirePermissionRejectsInvalidResourceID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	checker := &permissionCheckerStub{allowed: true}
	router := newPermissionRouter(checker, ResourceTask, "task.read", 42)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/not-a-number", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want %d", rec.Code, http.StatusBadRequest)
	}
	if checker.calls != 0 {
		t.Fatalf("checker calls=%d want 0", checker.calls)
	}
}

func TestRequirePermissionRejectsDeniedAccess(t *testing.T) {
	gin.SetMode(gin.TestMode)

	checker := &permissionCheckerStub{allowed: false}
	handlerCalled := false
	router := newPermissionRouterWithHandler(checker, ResourceSpace, "space.update", 42, func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/10", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d want %d: %s", rec.Code, http.StatusForbidden, rec.Body.String())
	}
	if checker.calls != 1 {
		t.Fatalf("checker calls=%d want 1", checker.calls)
	}
	assertMiddlewareErrorCode(t, rec, "FORBIDDEN")
	if handlerCalled {
		t.Fatal("downstream handler was called after permission denial")
	}
}

func TestRequirePermissionReturns500WhenCheckerFails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	checkerErr := errors.New("database unavailable")
	checker := &permissionCheckerStub{allowed: true, err: checkerErr}
	handlerCalled := false
	router := newPermissionRouterWithHandler(checker, ResourceList, "task.read", 42, func(c *gin.Context) {
		handlerCalled = true
		c.Status(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/resource/10", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want %d: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
	if checker.calls != 1 {
		t.Fatalf("checker calls=%d want 1", checker.calls)
	}
	assertMiddlewareErrorCode(t, rec, "INTERNAL_ERROR")
	if handlerCalled {
		t.Fatal("downstream handler was called after checker failure")
	}
}

func newPermissionRouter(checker service.PermissionChecker, resource Resource, code string, userID uint) *gin.Engine {
	return newPermissionRouterWithHandler(checker, resource, code, userID, func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
}

func newPermissionRouterWithHandler(checker service.PermissionChecker, resource Resource, code string, userID uint, next gin.HandlerFunc) *gin.Engine {
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	router.GET(permissionTestPathPattern(resource), RequirePermission(checker, resource, code), next)
	return router
}

func permissionTestPath(id uint) string {
	return "/resource/" + strconv.FormatUint(uint64(id), 10)
}

func permissionTestPathPattern(resource Resource) string {
	return "/resource/:" + string(resource)
}

func assertMiddlewareErrorCode(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()

	var payload response.APIResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if payload.Error == nil || payload.Error.Code != want {
		t.Fatalf("error code=%v want %q; body=%s", payload.Error, want, rec.Body.String())
	}
}
