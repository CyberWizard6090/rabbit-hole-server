package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/service"
)

type taskHandlerServiceStub struct {
	params pagination.Params
	listID uint
	called bool
}

func (s *taskHandlerServiceStub) CreateTask(uint, service.CreateTaskParams) (*domain.Task, error) {
	return nil, nil
}

func (s *taskHandlerServiceStub) ListTasks(listID uint, params pagination.Params) (pagination.Response[domain.Task], error) {
	s.called = true
	s.listID = listID
	s.params = params
	return pagination.Response[domain.Task]{Items: []domain.Task{}}, nil
}

func (s *taskHandlerServiceStub) GetTaskByID(uint) (*domain.Task, error) {
	return nil, nil
}

func (s *taskHandlerServiceStub) UpdateTask(uint, service.UpdateTaskParams) (*domain.Task, error) {
	return nil, nil
}

func (s *taskHandlerServiceStub) DeleteTask(uint) error {
	return nil
}

func TestTaskHandlerListParsesCursorParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	codec, err := pagination.NewCodec([]byte("handler-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := pagination.EncodePosition(codec, pagination.TaskCursorContext(7), pagination.DirectionNext, pagination.TaskCursorPosition{
		CreatedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		ID:        42,
	})
	if err != nil {
		t.Fatal(err)
	}

	svc := &taskHandlerServiceStub{}
	handler := NewTaskHandler(svc, codec)
	request := httptest.NewRequest(http.MethodGet, "/lists/7/tasks?limit=25&cursor="+token, nil)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	context.Params = gin.Params{{Key: "list_id", Value: "7"}}
	context.Set("userID", uint(1))

	handler.List(context)

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
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	context.Params = gin.Params{{Key: "list_id", Value: "7"}}
	context.Set("userID", uint(1))

	handler.List(context)

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
	token, err := pagination.EncodePosition(codec, pagination.TaskCursorContext(8), pagination.DirectionNext, pagination.TaskCursorPosition{
		CreatedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC),
		ID:        42,
	})
	if err != nil {
		t.Fatal(err)
	}
	svc := &taskHandlerServiceStub{}
	handler := NewTaskHandler(svc, codec)
	request := httptest.NewRequest(http.MethodGet, "/lists/7/tasks?cursor="+token, nil)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	context.Params = gin.Params{{Key: "list_id", Value: "7"}}
	context.Set("userID", uint(1))

	handler.List(context)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if svc.called {
		t.Fatal("service must not be called for cursor from another list")
	}
}
