package service

import (
	"context"
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func newMoveService(task *domain.Task) (TaskService, *taskRepositoryMock) {
	repo := &taskRepositoryMock{task: task}
	return newTestTaskService(repo, &spaceRepositoryMock{}, &tagServiceMock{}), repo
}

func TestTaskServiceMoveTaskKeepsCurrentListAndStatusByDefault(t *testing.T) {
	svc, repo := newMoveService(&domain.Task{Model: domain.Model{ID: 5}, ListID: 7, StatusID: 3})
	after := uint(9)

	if _, err := svc.MoveTask(context.Background(), 7, 5, MoveTaskParams{AfterID: &after}); err != nil {
		t.Fatalf("MoveTask error=%v", err)
	}
	got := repo.movedTarget
	if got.ListID != 7 || got.StatusID != 3 || got.AfterID == nil || *got.AfterID != 9 || got.BeforeID != nil {
		t.Fatalf("target = %+v", got)
	}
}

func TestTaskServiceMoveTaskToAnotherListRequiresStatus(t *testing.T) {
	svc, repo := newMoveService(&domain.Task{Model: domain.Model{ID: 5}, ListID: 7, StatusID: 3})
	list, status := uint(8), uint(21)

	if _, err := svc.MoveTask(context.Background(), 7, 5, MoveTaskParams{ListID: &list}); !errors.Is(err, domain.ErrInvalidStatusReference) {
		t.Fatalf("err=%v", err)
	}
	if _, err := svc.MoveTask(context.Background(), 7, 5, MoveTaskParams{ListID: &list, StatusID: &status}); err != nil {
		t.Fatalf("MoveTask error=%v", err)
	}
	if repo.movedTarget.ListID != 8 || repo.movedTarget.StatusID != 21 {
		t.Fatalf("target = %+v", repo.movedTarget)
	}
}

func TestTaskServiceMoveTaskSameListIdIsNotACrossListMove(t *testing.T) {
	svc, repo := newMoveService(&domain.Task{Model: domain.Model{ID: 5}, ListID: 7, StatusID: 3})
	list := uint(7)

	if _, err := svc.MoveTask(context.Background(), 7, 5, MoveTaskParams{ListID: &list}); err != nil {
		t.Fatalf("MoveTask error=%v", err)
	}
	if repo.movedTarget.ListID != 7 || repo.movedTarget.StatusID != 3 {
		t.Fatalf("target = %+v", repo.movedTarget)
	}
}

func TestTaskServiceMoveTaskRejectsBadAnchors(t *testing.T) {
	svc, repo := newMoveService(&domain.Task{Model: domain.Model{ID: 5}, ListID: 7, StatusID: 3})
	self, other := uint(5), uint(6)

	for name, params := range map[string]MoveTaskParams{
		"after self":  {AfterID: &self},
		"before self": {BeforeID: &self},
		"both":        {AfterID: &other, BeforeID: &other},
	} {
		if _, err := svc.MoveTask(context.Background(), 7, 5, params); !errors.Is(err, domain.ErrInvalidMoveAnchor) {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if repo.movedTarget != (domain.TaskMove{}) {
		t.Fatalf("repository was called: %+v", repo.movedTarget)
	}
}

func TestTaskServiceMoveTaskPropagatesRepositoryErrors(t *testing.T) {
	svc, repo := newMoveService(&domain.Task{Model: domain.Model{ID: 5}, ListID: 7, StatusID: 3})
	repo.moveErr = domain.ErrTaskHasSubtasks

	if _, err := svc.MoveTask(context.Background(), 7, 5, MoveTaskParams{}); !errors.Is(err, domain.ErrTaskHasSubtasks) {
		t.Fatalf("err=%v", err)
	}
}
