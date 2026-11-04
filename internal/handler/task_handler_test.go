package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/service"
)

type taskHandlerServiceStub struct {
	params pagination.Params
	query  domain.TaskQuery
	listID uint
	called bool

	getTask *domain.Task
	getErr  error
}

func (s *taskHandlerServiceStub) CreateTask(context.Context, uint, service.CreateTaskParams) (*domain.Task, error) {
	return nil, nil
}

func (s *taskHandlerServiceStub) ListTasks(_ context.Context, listID uint, params pagination.Params, query domain.TaskQuery) (pagination.Response[domain.Task], error) {
	s.called = true
	s.listID = listID
	s.params = params
	s.query = query
	return pagination.Response[domain.Task]{Items: []domain.Task{}}, nil
}

func (s *taskHandlerServiceStub) GetTaskByID(context.Context, uint) (*domain.Task, error) {
	return s.getTask, s.getErr
}

func (s *taskHandlerServiceStub) UpdateTask(context.Context, uint, service.UpdateTaskParams) (*domain.Task, error) {
	return nil, nil
}

func (s *taskHandlerServiceStub) DeleteTask(context.Context, uint) error {
	return nil
}

func (s *taskHandlerServiceStub) MoveTask(context.Context, uint, service.MoveTaskParams) (*domain.Task, error) {
	return nil, nil
}

func TestTaskHandlerListParsesCursorParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codec, err := pagination.NewCodec([]byte("handler-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := pagination.EncodePosition(codec, pagination.TaskCursorContext(7, "id_desc", ""), pagination.DirectionNext, pagination.TaskCursorPosition{Keys: []json.RawMessage{json.RawMessage("42")}})
	if err != nil {
		t.Fatal(err)
	}

	svc := &taskHandlerServiceStub{}
	handler := NewTaskHandler(svc, codec)
	request := httptest.NewRequest(http.MethodGet, "/lists/7/tasks?limit=25&cursor="+token, nil)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = request
	ginCtx.Params = gin.Params{{Key: "list_id", Value: "7"}}
	ginCtx.Set("userID", uint(1))

	handler.List(ginCtx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d, body=%s", recorder.Code, recorder.Body.String())
	}
	if !svc.called || svc.listID != 7 || svc.params.Limit != 25 {
		t.Fatalf("service call: called=%t listID=%d params=%+v", svc.called, svc.listID, svc.params)
	}
	if svc.params.Cursor == nil || svc.params.Cursor.Direction != pagination.DirectionNext {
		t.Fatalf("cursor params=%+v", svc.params.Cursor)
	}
}

func TestTaskHandlerListRejectsInvalidCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codec, err := pagination.NewCodec([]byte("handler-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &taskHandlerServiceStub{}
	handler := NewTaskHandler(svc, codec)
	request := httptest.NewRequest(http.MethodGet, "/lists/7/tasks?cursor=invalid", nil)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = request
	ginCtx.Params = gin.Params{{Key: "list_id", Value: "7"}}
	ginCtx.Set("userID", uint(1))

	handler.List(ginCtx)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d, body=%s", recorder.Code, recorder.Body.String())
	}
	if svc.called {
		t.Fatal("service should not be called for an invalid cursor")
	}
}

func TestTaskHandlerListRejectsCursorFromAnotherList(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codec, err := pagination.NewCodec([]byte("handler-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := pagination.EncodePosition(codec, pagination.TaskCursorContext(8, "id_desc", ""), pagination.DirectionNext, pagination.TaskCursorPosition{Keys: []json.RawMessage{json.RawMessage("42")}})
	if err != nil {
		t.Fatal(err)
	}
	svc := &taskHandlerServiceStub{}
	handler := NewTaskHandler(svc, codec)
	request := httptest.NewRequest(http.MethodGet, "/lists/7/tasks?cursor="+token, nil)
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = request
	ginCtx.Params = gin.Params{{Key: "list_id", Value: "7"}}
	ginCtx.Set("userID", uint(1))

	handler.List(ginCtx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if svc.called {
		t.Fatal("service must not be called for cursor from another list")
	}
}

func getTaskStatus(t *testing.T, svc *taskHandlerServiceStub) (int, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	codec, err := pagination.NewCodec([]byte("handler-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	ginCtx.Request = httptest.NewRequest(http.MethodGet, "/tasks/5", nil)
	ginCtx.Params = gin.Params{{Key: "task_id", Value: "5"}}
	ginCtx.Set("userID", uint(1))

	NewTaskHandler(svc, codec).Get(ginCtx)
	return recorder.Code, recorder.Body.String()
}

func TestTaskHandlerGetMapsMissingRowTo404(t *testing.T) {
	code, body := getTaskStatus(t, &taskHandlerServiceStub{getErr: domain.ErrNotFound})
	if code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s, want 404", code, body)
	}
}

func TestTaskHandlerGetDoesNotReportDatabaseFailureAs404(t *testing.T) {
	code, body := getTaskStatus(t, &taskHandlerServiceStub{getErr: errors.New("connection refused")})
	if code != http.StatusInternalServerError || strings.Contains(body, "connection refused") {
		t.Fatalf("status=%d body=%s, want 500 without leaking the cause", code, body)
	}
}

func TestTaskHandlerGetReturnsTask(t *testing.T) {
	code, _ := getTaskStatus(t, &taskHandlerServiceStub{getTask: &domain.Task{Title: "x"}})
	if code != http.StatusOK {
		t.Fatalf("status=%d, want 200", code)
	}
}
