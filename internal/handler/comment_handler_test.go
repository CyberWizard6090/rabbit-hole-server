package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/realtime"
	"rabbit-hole-server/internal/service"
)

type commentServiceStub struct {
	comment *domain.Comment
	err     error
	body    string
	ids     []uint
}

func (s *commentServiceStub) CreateComment(_ context.Context, _, _ uint, body string, mentionIDs []uint) (*domain.Comment, error) {
	s.body, s.ids = body, mentionIDs
	return s.comment, s.err
}
func (s *commentServiceStub) ListComments(context.Context, uint, int, int) ([]domain.Comment, int64, error) {
	return []domain.Comment{}, 0, s.err
}
func (s *commentServiceStub) UpdateComment(context.Context, uint, uint, uint, *string, *[]uint) (*domain.Comment, error) {
	return s.comment, s.err
}
func (s *commentServiceStub) DeleteComment(context.Context, uint, uint, uint) error { return s.err }

func commentRouter(comments service.CommentService, tasks service.TaskService, pub TaskEventPublisher) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewCommentHandler(comments, tasks).WithPublisher(pub)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uint(42)) })
	r.POST("/tasks/:task_id/comments", h.Create)
	r.GET("/tasks/:task_id/comments", h.List)
	r.PATCH("/tasks/:task_id/comments/:comment_id", h.Update)
	r.DELETE("/tasks/:task_id/comments/:comment_id", h.Delete)
	return r
}

func TestCommentHandlerPublishesCreatedUpdatedAndDeleted(t *testing.T) {
	pub := &recordingPublisher{}
	comment := &domain.Comment{ID: 8, TaskID: 5, Body: "hi"}
	r := commentRouter(&commentServiceStub{comment: comment}, &eventsServiceStub{existing: taskWith(5, 2, 7)}, pub)

	if rec := do(r, http.MethodPost, "/tasks/5/comments", `{"body":"  hi  ","mention_user_ids":[3]}`); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(r, http.MethodPatch, "/tasks/5/comments/8", `{"body":"edited"}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(r, http.MethodDelete, "/tasks/5/comments/8", ``); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}

	if len(pub.events) != 3 {
		t.Fatalf("events = %+v", pub.events)
	}
	for i, want := range []string{realtime.EventCommentCreated, realtime.EventCommentUpdated, realtime.EventCommentDeleted} {
		e := pub.events[i]
		if e.Type != want || e.TaskID != 5 || e.ListID != 7 || e.SpaceID != 2 || e.CommentID != 8 || e.ActorID != 42 {
			t.Errorf("event %d = %+v", i, e)
		}
	}
	if pub.events[2].Comment != nil {
		t.Error("deleted event must not carry the comment")
	}
}

func TestCommentHandlerTrimsBodyAndRejectsBlank(t *testing.T) {
	svc := &commentServiceStub{comment: &domain.Comment{ID: 1}}
	r := commentRouter(svc, &eventsServiceStub{}, nil)

	if rec := do(r, http.MethodPost, "/tasks/5/comments", `{"body":"  hello  "}`); rec.Code != http.StatusCreated || svc.body != "hello" {
		t.Fatalf("create: %d body=%q", rec.Code, svc.body)
	}
	for _, body := range []string{`{"body":"   "}`, `{}`, `{"body":"x","mention_user_ids":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51]}`} {
		if rec := do(r, http.MethodPost, "/tasks/5/comments", body); rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want a validation error", body, rec.Code)
		}
	}
}

func TestCommentHandlerMapsErrorsAndPublishesNothing(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"missing":   {domain.ErrNotFound, http.StatusNotFound},
		"forbidden": {domain.ErrCommentForbidden, http.StatusForbidden},
		"mention":   {domain.ErrInvalidMentionReference, http.StatusBadRequest},
		"other":     {errors.New("boom"), http.StatusInternalServerError},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pub := &recordingPublisher{}
			r := commentRouter(&commentServiceStub{err: tc.err}, &eventsServiceStub{existing: taskWith(5, 2, 7)}, pub)
			for _, call := range []struct{ method, path, body string }{
				{http.MethodPost, "/tasks/5/comments", `{"body":"hi"}`},
				{http.MethodPatch, "/tasks/5/comments/8", `{"body":"hi"}`},
				{http.MethodDelete, "/tasks/5/comments/8", ``},
			} {
				if rec := do(r, call.method, call.path, call.body); rec.Code != tc.want {
					t.Errorf("%s %s: status %d, want %d", call.method, call.path, rec.Code, tc.want)
				}
			}
			if len(pub.events) != 0 {
				t.Fatalf("failed calls published %+v", pub.events)
			}
		})
	}
}

func TestCommentHandlerRejectsBadIDs(t *testing.T) {
	r := commentRouter(&commentServiceStub{}, &eventsServiceStub{}, nil)
	for _, path := range []string{"/tasks/abc/comments", "/tasks/0/comments"} {
		if rec := do(r, http.MethodGet, path, ``); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s: status %d, want 400", path, rec.Code)
		}
	}
	if rec := do(r, http.MethodDelete, "/tasks/5/comments/x", ``); rec.Code != http.StatusBadRequest {
		t.Errorf("bad comment id: status %d, want 400", rec.Code)
	}
}
