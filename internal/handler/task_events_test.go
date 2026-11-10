package handler

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

type recordingPublisher struct{ events []realtime.Event }

func (p *recordingPublisher) Publish(e realtime.Event) { p.events = append(p.events, e) }

type eventsServiceStub struct {
	created, updated, existing *domain.Task
	createErr, updateErr       error
	getErr, deleteErr          error
	moved                      *domain.Task
	moveErr                    error
	movedWith                  service.MoveTaskParams
	calls                      []string
}

func (s *eventsServiceStub) CreateTask(context.Context, uint, service.CreateTaskParams) (*domain.Task, error) {
	return s.created, s.createErr
}
func (s *eventsServiceStub) UpdateTask(context.Context, uint, uint, service.UpdateTaskParams) (*domain.Task, error) {
	return s.updated, s.updateErr
}
func (s *eventsServiceStub) GetTaskByID(context.Context, uint) (*domain.Task, error) {
	s.calls = append(s.calls, "get")
	return s.existing, s.getErr
}
func (s *eventsServiceStub) DeleteTask(context.Context, uint) error {
	s.calls = append(s.calls, "delete")
	return s.deleteErr
}
func (s *eventsServiceStub) MoveTask(_ context.Context, _, _ uint, params service.MoveTaskParams) (*domain.Task, error) {
	s.movedWith = params
	return s.moved, s.moveErr
}
func (s *eventsServiceStub) ListTasks(context.Context, uint, pagination.Params, domain.TaskQuery) (pagination.Response[domain.Task], error) {
	return pagination.Response[domain.Task]{}, nil
}

func taskWith(id, spaceID, listID uint) *domain.Task {
	task := &domain.Task{SpaceID: spaceID, ListID: listID, Title: "task"}
	task.ID = id
	return task
}

func eventsRouter(t *testing.T, svc service.TaskService, publisher TaskEventPublisher) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	codec, err := pagination.NewCodec([]byte("handler-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewTaskHandler(svc, codec)
	if publisher != nil {
		h.WithPublisher(publisher)
	}
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uint(42)) })
	r.POST("/lists/:list_id/tasks", h.Create)
	r.PATCH("/tasks/:task_id", h.Update)
	r.DELETE("/tasks/:task_id", h.Delete)
	r.POST("/tasks/:task_id/move", h.Move)
	return r
}

func do(r http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestTaskHandlerPublishesCreatedUpdatedAndDeleted(t *testing.T) {
	pub := &recordingPublisher{}
	svc := &eventsServiceStub{created: taskWith(5, 2, 7), updated: taskWith(5, 2, 7), existing: taskWith(5, 2, 7)}
	r := eventsRouter(t, svc, pub)

	if rec := do(r, http.MethodPost, "/lists/7/tasks", `{"title":"a task","status_id":1}`); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(r, http.MethodPatch, "/tasks/5", `{"title":"renamed"}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(r, http.MethodDelete, "/tasks/5", ``); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}

	if len(pub.events) != 3 {
		t.Fatalf("events = %+v, want 3", pub.events)
	}
	for i, wantType := range []string{realtime.EventTaskCreated, realtime.EventTaskUpdated, realtime.EventTaskDeleted} {
		e := pub.events[i]
		if e.Type != wantType || e.TaskID != 5 || e.ListID != 7 || e.SpaceID != 2 || e.ActorID != 42 {
			t.Errorf("event %d = %+v", i, e)
		}
	}
	if pub.events[0].Task == nil || pub.events[1].Task == nil {
		t.Error("created/updated events must carry the task")
	}
	if pub.events[2].Task != nil {
		t.Error("deleted event must not carry a task payload")
	}
}

func TestTaskHandlerReadsTheTaskBeforeDeletingIt(t *testing.T) {
	svc := &eventsServiceStub{existing: taskWith(5, 2, 7)}
	r := eventsRouter(t, svc, &recordingPublisher{})

	do(r, http.MethodDelete, "/tasks/5", ``)
	if len(svc.calls) != 2 || svc.calls[0] != "get" || svc.calls[1] != "delete" {
		t.Fatalf("calls = %v, want the list to be looked up before the delete", svc.calls)
	}
}

func TestTaskHandlerPublishesNothingWhenTheOperationFails(t *testing.T) {
	boom := errors.New("boom")
	pub := &recordingPublisher{}
	svc := &eventsServiceStub{createErr: boom, updateErr: boom, existing: taskWith(5, 2, 7), deleteErr: boom}
	r := eventsRouter(t, svc, pub)

	do(r, http.MethodPost, "/lists/7/tasks", `{"title":"a task","status_id":1}`)
	do(r, http.MethodPost, "/lists/7/tasks", `{"title":""}`)
	do(r, http.MethodPatch, "/tasks/5", `{"title":"renamed"}`)
	do(r, http.MethodDelete, "/tasks/5", ``)

	if len(pub.events) != 0 {
		t.Fatalf("failed operations published %+v", pub.events)
	}
}

func TestTaskHandlerWorksWithoutAPublisher(t *testing.T) {
	svc := &eventsServiceStub{created: taskWith(5, 2, 7), updated: taskWith(5, 2, 7)}
	r := eventsRouter(t, svc, nil)

	if rec := do(r, http.MethodPost, "/lists/7/tasks", `{"title":"a task","status_id":1}`); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	if rec := do(r, http.MethodDelete, "/tasks/5", ``); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d", rec.Code)
	}
	if len(svc.calls) != 1 || svc.calls[0] != "delete" {
		t.Fatalf("without a publisher the extra lookup must be skipped, calls = %v", svc.calls)
	}
}

func TestTaskHandlerStillDeletesWhenTheLookupFails(t *testing.T) {
	pub := &recordingPublisher{}
	svc := &eventsServiceStub{getErr: domain.ErrNotFound}
	r := eventsRouter(t, svc, pub)

	if rec := do(r, http.MethodDelete, "/tasks/5", ``); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if len(pub.events) != 0 {
		t.Fatalf("no list is known, so no event can be routed: %+v", pub.events)
	}
}
