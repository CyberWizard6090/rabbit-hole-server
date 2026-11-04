package handler

import (
	"context"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/realtime"
)

type permissionCheckerStub struct {
	allow     bool
	listCalls []uint
	code      string
}

func (p *permissionCheckerStub) HasWorkspacePermission(context.Context, uint, uint, string) (bool, error) {
	return true, nil
}
func (p *permissionCheckerStub) HasSpacePermission(context.Context, uint, uint, string) (bool, error) {
	return true, nil
}
func (p *permissionCheckerStub) HasFolderPermission(context.Context, uint, uint, string) (bool, error) {
	return true, nil
}
func (p *permissionCheckerStub) HasListPermission(_ context.Context, _ uint, listID uint, code string) (bool, error) {
	p.listCalls = append(p.listCalls, listID)
	p.code = code
	return p.allow, nil
}
func (p *permissionCheckerStub) HasTaskPermission(context.Context, uint, uint, string) (bool, error) {
	return true, nil
}

func moveRouter(t *testing.T, svc *eventsServiceStub, pub TaskEventPublisher, perms *permissionCheckerStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	codec, err := pagination.NewCodec([]byte("handler-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	h := NewTaskHandler(svc, codec).WithPublisher(pub).WithPermissionChecker(perms)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("userID", uint(42)) })
	r.POST("/tasks/:task_id/move", h.Move)
	return r
}

func TestTaskHandlerMoveWithinListPublishesUpdateWithoutTargetCheck(t *testing.T) {
	pub := &recordingPublisher{}
	perms := &permissionCheckerStub{}
	svc := &eventsServiceStub{existing: taskWith(5, 2, 7), moved: taskWith(5, 2, 7)}
	r := moveRouter(t, svc, pub, perms)

	rec := do(r, http.MethodPost, "/tasks/5/move", `{"status_id":3,"after_id":9}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	if len(perms.listCalls) != 0 {
		t.Fatalf("same-list move checked list permissions: %v", perms.listCalls)
	}
	if svc.movedWith.StatusID == nil || *svc.movedWith.StatusID != 3 || svc.movedWith.AfterID == nil || *svc.movedWith.AfterID != 9 {
		t.Fatalf("service params = %+v", svc.movedWith)
	}
	if len(pub.events) != 1 || pub.events[0].Type != realtime.EventTaskUpdated {
		t.Fatalf("events = %+v", pub.events)
	}
}

func TestTaskHandlerMoveToAnotherListNeedsCreatePermissionThere(t *testing.T) {
	pub := &recordingPublisher{}
	perms := &permissionCheckerStub{allow: false}
	svc := &eventsServiceStub{existing: taskWith(5, 2, 7), moved: taskWith(5, 2, 8)}
	r := moveRouter(t, svc, pub, perms)

	rec := do(r, http.MethodPost, "/tasks/5/move", `{"list_id":8,"status_id":3}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
	if len(perms.listCalls) != 1 || perms.listCalls[0] != 8 || perms.code != domain.PermTaskCreate {
		t.Fatalf("checks = %v code=%s", perms.listCalls, perms.code)
	}
	if svc.movedWith.ListID != nil || len(pub.events) != 0 {
		t.Fatal("a forbidden move reached the service or published events")
	}
}

func TestTaskHandlerMoveToAnotherListPublishesDeleteThenCreate(t *testing.T) {
	pub := &recordingPublisher{}
	svc := &eventsServiceStub{existing: taskWith(5, 2, 7), moved: taskWith(5, 3, 8)}
	r := moveRouter(t, svc, pub, &permissionCheckerStub{allow: true})

	if rec := do(r, http.MethodPost, "/tasks/5/move", `{"list_id":8,"status_id":3}`); rec.Code != http.StatusOK {
		t.Fatalf("move: %d %s", rec.Code, rec.Body.String())
	}
	if len(pub.events) != 2 {
		t.Fatalf("events = %+v", pub.events)
	}
	if e := pub.events[0]; e.Type != realtime.EventTaskDeleted || e.ListID != 7 || e.SpaceID != 2 || e.Task != nil {
		t.Errorf("first event = %+v", e)
	}
	if e := pub.events[1]; e.Type != realtime.EventTaskCreated || e.ListID != 8 || e.SpaceID != 3 || e.Task == nil {
		t.Errorf("second event = %+v", e)
	}
}

func TestTaskHandlerMoveMapsErrors(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"subtasks":   {domain.ErrTaskHasSubtasks, http.StatusConflict},
		"concurrent": {domain.ErrConcurrentMove, http.StatusConflict},
		"anchor":     {domain.ErrInvalidMoveAnchor, http.StatusBadRequest},
		"list":       {domain.ErrInvalidListReference, http.StatusBadRequest},
		"status":     {domain.ErrInvalidStatusReference, http.StatusBadRequest},
		"missing":    {domain.ErrNotFound, http.StatusNotFound},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			pub := &recordingPublisher{}
			svc := &eventsServiceStub{existing: taskWith(5, 2, 7), moveErr: tc.err}
			r := moveRouter(t, svc, pub, &permissionCheckerStub{allow: true})
			rec := do(r, http.MethodPost, "/tasks/5/move", `{"status_id":3}`)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.want, rec.Body.String())
			}
			if len(pub.events) != 0 {
				t.Fatalf("failed move published %+v", pub.events)
			}
		})
	}
}

func TestTaskHandlerMoveRejectsBothAnchors(t *testing.T) {
	svc := &eventsServiceStub{existing: taskWith(5, 2, 7), moved: taskWith(5, 2, 7)}
	r := moveRouter(t, svc, &recordingPublisher{}, &permissionCheckerStub{allow: true})
	if rec := do(r, http.MethodPost, "/tasks/5/move", `{"after_id":1,"before_id":2}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
