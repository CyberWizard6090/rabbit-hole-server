package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
)

func newTestTaskService(
	repo domain.TaskRepository,
	spaceRepo domain.SpaceRepository,
	tagService TagService,
) TaskService {
	codec, err := pagination.NewCodec([]byte("test-cursor-secret-at-least-32-bytes"))
	if err != nil {
		panic(err)
	}
	return NewTaskService(repo, spaceRepo, tagService, codec)
}

func TestTaskServiceCreateTaskUsesDefaultsAndResolvesNewTags(t *testing.T) {
	tagMock := &tagServiceMock{results: map[string]*domain.Tag{
		"Bug":  {ID: 10, SpaceID: 55, Name: "Bug"},
		"Idea": {ID: 11, SpaceID: 55, Name: "Idea"},
	}}
	taskRepo := &taskRepositoryMock{}
	spaceRepo := &spaceRepositoryMock{list: &domain.List{Model: gorm.Model{ID: 7}, SpaceID: 55}}
	svc := newTestTaskService(taskRepo, spaceRepo, tagMock)

	task, err := svc.CreateTask(42, CreateTaskParams{
		Title:       "Build feature",
		Description: "desc",
		ListID:      7,
		StatusID:    3,
		TagIDs:      []uint{9},
		NewTagNames: []string{" Bug ", "", "Idea"},
		Priority:    0,
	})
	if err != nil {
		t.Fatalf("CreateTask error=%v", err)
	}
	if task.Priority != 2 || task.SpaceID != 55 || task.ListID != 7 || task.UserID != 42 {
		t.Fatalf("task=%+v", task)
	}
	if !reflect.DeepEqual(tagMock.calls, []string{"Bug", "Idea"}) {
		t.Fatalf("tag calls=%v", tagMock.calls)
	}
	if !reflect.DeepEqual(taskRepo.tagIDs, []uint{9, 10, 11}) {
		t.Fatalf("tag ids=%v", taskRepo.tagIDs)
	}
	if !reflect.DeepEqual(taskRepo.assigneeIDs, []uint(nil)) {
		t.Fatalf("assignee ids=%v", taskRepo.assigneeIDs)
	}
	if taskRepo.createdTask != task {
		t.Fatal("repository did not receive returned task")
	}
}

func TestTaskServiceCreateTaskPreservesExplicitPriority(t *testing.T) {
	taskRepo := &taskRepositoryMock{}
	spaceRepo := &spaceRepositoryMock{list: &domain.List{Model: gorm.Model{ID: 7}, SpaceID: 55}}
	svc := newTestTaskService(taskRepo, spaceRepo, &tagServiceMock{})

	task, err := svc.CreateTask(42, CreateTaskParams{Title: "Task", ListID: 7, StatusID: 3, Priority: 5})
	if err != nil {
		t.Fatalf("CreateTask error=%v", err)
	}
	if task.Priority != 5 {
		t.Fatalf("priority=%d want 5", task.Priority)
	}
}

func TestTaskServiceCreateTaskStopsWhenListLookupFails(t *testing.T) {
	wantErr := errors.New("list not found")
	repo := &taskRepositoryMock{}
	svc := newTestTaskService(repo, &spaceRepositoryMock{getListErr: wantErr}, &tagServiceMock{})

	if _, err := svc.CreateTask(42, CreateTaskParams{ListID: 7}); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
	if repo.createdTask != nil {
		t.Fatal("task repository should not be called when list lookup fails")
	}
}

func TestTaskServiceCreateTaskStopsWhenNewTagResolutionFails(t *testing.T) {
	wantErr := errors.New("tag service failed")
	repo := &taskRepositoryMock{}
	spaceRepo := &spaceRepositoryMock{list: &domain.List{Model: gorm.Model{ID: 7}, SpaceID: 55}}
	svc := newTestTaskService(repo, spaceRepo, &tagServiceMock{err: wantErr})

	if _, err := svc.CreateTask(42, CreateTaskParams{ListID: 7, NewTagNames: []string{"Bug"}}); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
	if repo.createdTask != nil {
		t.Fatal("task should not be created after tag resolution failure")
	}
}

func TestTaskServiceCreateTaskPropagatesRepositoryCreateError(t *testing.T) {
	wantErr := errors.New("create task failed")
	taskRepo := &taskRepositoryMock{createErr: wantErr}
	spaceRepo := &spaceRepositoryMock{list: &domain.List{Model: gorm.Model{ID: 7}, SpaceID: 55}}
	svc := newTestTaskService(taskRepo, spaceRepo, &tagServiceMock{})

	if _, err := svc.CreateTask(42, CreateTaskParams{ListID: 7, StatusID: 3, Title: "Task"}); !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
}

func TestTaskServiceListTasksEncodesCursors(t *testing.T) {
	tasks := []domain.Task{
		{Model: gorm.Model{ID: 9, CreatedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}},
		{Model: gorm.Model{ID: 8, CreatedAt: time.Date(2026, 10, 9, 11, 0, 0, 0, time.UTC)}},
	}
	taskRepo := &taskRepositoryMock{task: &tasks[0], tasks: tasks, hasNext: true, hasPrev: true}
	svc := newTestTaskService(taskRepo, &spaceRepositoryMock{}, &tagServiceMock{})

	got, err := svc.ListTasks(7, pagination.Params{Limit: 2})
	if err != nil || !reflect.DeepEqual(got.Items, tasks) || !got.HasNext || !got.HasPrev {
		t.Fatalf("ListTasks=%#v err=%v", got, err)
	}
	if got.NextCursor == nil || got.PrevCursor == nil {
		t.Fatal("expected both cursors")
	}
	codec, err := pagination.NewCodec([]byte("test-cursor-secret-at-least-32-bytes"))
	if err != nil {
		t.Fatal(err)
	}
	next, err := codec.Decode(*got.NextCursor)
	context := pagination.TaskCursorContext(7)
	if err != nil || next.Direction != pagination.DirectionNext || next.Resource != context.Resource || next.Scope != context.Scope || next.Sort != context.Sort {
		t.Fatalf("next cursor=(%+v, %v)", next, err)
	}
	nextPosition, err := pagination.DecodePosition[pagination.TaskCursorPosition](next, context)
	if err != nil || nextPosition.ID != 8 {
		t.Fatalf("next position=(%+v, %v)", nextPosition, err)
	}
	prev, err := codec.Decode(*got.PrevCursor)
	if err != nil || prev.Direction != pagination.DirectionPrev || prev.Resource != context.Resource || prev.Scope != context.Scope || prev.Sort != context.Sort {
		t.Fatalf("previous cursor=(%+v, %v)", prev, err)
	}
	prevPosition, err := pagination.DecodePosition[pagination.TaskCursorPosition](prev, context)
	if err != nil || prevPosition.ID != 9 {
		t.Fatalf("previous position=(%+v, %v)", prevPosition, err)
	}

	firstPageRepo := &taskRepositoryMock{tasks: tasks, hasNext: true}
	firstPageService := newTestTaskService(firstPageRepo, &spaceRepositoryMock{}, &tagServiceMock{})
	firstPage, err := firstPageService.ListTasks(7, pagination.Params{Limit: 2})
	if err != nil || firstPage.HasPrev || firstPage.PrevCursor != nil {
		t.Fatalf("first page should have no previous cursor: response=%#v err=%v", firstPage, err)
	}

	gotTask, err := svc.GetTaskByID(1)
	if err != nil || gotTask != &tasks[0] {
		t.Fatalf("GetTaskByID=%+v err=%v", gotTask, err)
	}
}

func TestTaskServiceUpdateTaskChangesFieldsAndAddsTags(t *testing.T) {
	title, description := "New title", "New description"
	statusID, priority, estimate := uint(8), 4, 120
	task := &domain.Task{Model: gorm.Model{ID: 9}, SpaceID: 55, ListID: 7, Title: "Old", Description: "Old desc", StatusID: 3, Priority: 2, TimeEstimate: 10}
	taskRepo := &taskRepositoryMock{task: task}
	tagMock := &tagServiceMock{results: map[string]*domain.Tag{"Bug": {ID: 20, SpaceID: 55, Name: "Bug"}}}
	returned := &domain.Task{Model: gorm.Model{ID: 9}, SpaceID: 55, Title: "New title"}
	_ = returned
	svc := newTestTaskService(taskRepo, &spaceRepositoryMock{}, tagMock)

	got, err := svc.UpdateTask(9, UpdateTaskParams{
		Title: &title, Description: &description, StatusID: &statusID,
		Priority: &priority, TimeEstimate: &estimate, AddTagIDs: []uint{12}, AddTagNames: []string{"Bug"},
	})
	if err != nil {
		t.Fatalf("UpdateTask error=%v", err)
	}
	if got != taskRepo.task {
		t.Fatal("expected repository result to be returned")
	}
	if task.Title != title || task.Description != description || task.StatusID != 8 || task.Priority != 4 || task.TimeEstimate != 120 {
		t.Fatalf("updated task=%+v", task)
	}
	if !reflect.DeepEqual(taskRepo.addedTagIDs, []uint{12, 20}) || taskRepo.addedTaskID != 9 {
		t.Fatalf("added tags=(%d,%v)", taskRepo.addedTaskID, taskRepo.addedTagIDs)
	}
}

func TestTaskServiceUpdateTaskWithoutTagsDoesNotCallAddTags(t *testing.T) {
	task := &domain.Task{Model: gorm.Model{ID: 9}, SpaceID: 55, ListID: 7}
	taskRepo := &taskRepositoryMock{task: task}
	svc := newTestTaskService(taskRepo, &spaceRepositoryMock{}, &tagServiceMock{})

	if _, err := svc.UpdateTask(9, UpdateTaskParams{}); err != nil {
		t.Fatalf("UpdateTask error=%v", err)
	}
	if taskRepo.addedTaskID != 0 || taskRepo.addedTagIDs != nil {
		t.Fatalf("AddTags was unexpectedly called: id=%d tags=%v", taskRepo.addedTaskID, taskRepo.addedTagIDs)
	}
}

func TestTaskServiceUpdateTaskPropagatesErrors(t *testing.T) {
	getErr := errors.New("get failed")
	svc := newTestTaskService(&taskRepositoryMock{getErr: getErr}, &spaceRepositoryMock{}, &tagServiceMock{})
	if _, err := svc.UpdateTask(1, UpdateTaskParams{}); !errors.Is(err, getErr) {
		t.Fatalf("get error=%v", err)
	}

	updateErr := errors.New("update failed")
	task := &domain.Task{Model: gorm.Model{ID: 1}, SpaceID: 5}
	svc = newTestTaskService(&taskRepositoryMock{task: task, updateErr: updateErr}, &spaceRepositoryMock{}, &tagServiceMock{})
	if _, err := svc.UpdateTask(1, UpdateTaskParams{}); !errors.Is(err, updateErr) {
		t.Fatalf("update error=%v", err)
	}

	tagErr := errors.New("tag failed")
	updatedRepo := &taskRepositoryMock{task: &domain.Task{Model: gorm.Model{ID: 1}, SpaceID: 5}}
	svc = newTestTaskService(updatedRepo, &spaceRepositoryMock{}, &tagServiceMock{err: tagErr})
	if _, err := svc.UpdateTask(1, UpdateTaskParams{AddTagNames: []string{"Bug"}}); !errors.Is(err, tagErr) {
		t.Fatalf("tag error=%v", err)
	}
	if updatedRepo.updatedTask == nil {
		t.Fatal("task update was not attempted")
	}

	addErr := errors.New("add tags failed")
	updatedRepo = &taskRepositoryMock{task: &domain.Task{Model: gorm.Model{ID: 1}, SpaceID: 5}, addTagsErr: addErr}
	svc = newTestTaskService(updatedRepo, &spaceRepositoryMock{}, &tagServiceMock{})
	if _, err := svc.UpdateTask(1, UpdateTaskParams{AddTagIDs: []uint{3}}); !errors.Is(err, addErr) {
		t.Fatalf("add tags error=%v", err)
	}
}

func TestTaskServiceDeleteTask(t *testing.T) {
	wantErr := errors.New("delete failed")
	repo := &taskRepositoryMock{deleteErr: wantErr}
	svc := newTestTaskService(repo, &spaceRepositoryMock{}, &tagServiceMock{})
	if err := svc.DeleteTask(77); !errors.Is(err, wantErr) {
		t.Fatalf("DeleteTask error=%v", err)
	}
	if repo.deletedID != 77 {
		t.Fatalf("deleted id=%d want 77", repo.deletedID)
	}
}

func TestRandomTagColorReturnsPaletteValue(t *testing.T) {
	allowed := map[string]struct{}{}
	for _, color := range tagColorPalette {
		allowed[color] = struct{}{}
	}
	for i := 0; i < 50; i++ {
		if _, ok := allowed[randomTagColor()]; !ok {
			t.Fatalf("randomTagColor returned unexpected value")
		}
	}
}

func TestTaskServiceResolveNewTagsTrimsAndSkipsBlankNames(t *testing.T) {
	mock := &tagServiceMock{}
	svc := newTestTaskService(&taskRepositoryMock{}, &spaceRepositoryMock{}, mock)

	ids, err := svc.(*taskService).resolveNewTags(55, []string{"  Bug ", " ", "Idea"})
	if err != nil {
		t.Fatalf("resolveNewTags error=%v", err)
	}
	if !reflect.DeepEqual(mock.calls, []string{"Bug", "Idea"}) {
		t.Fatalf("calls=%v", mock.calls)
	}
	if !reflect.DeepEqual(ids, []uint{1, 2}) {
		t.Fatalf("ids=%v", ids)
	}
}

func TestTaskServiceResolveNewTagsWrapsErrorWithName(t *testing.T) {
	wantErr := errors.New("lookup failed")
	mock := &tagServiceMock{err: wantErr}
	svc := newTestTaskService(&taskRepositoryMock{}, &spaceRepositoryMock{}, mock).(*taskService)

	_, err := svc.resolveNewTags(55, []string{" Bug "})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(err.Error(), `get or create tag "Bug"`) {
		t.Fatalf("error=%q does not include tag name", err)
	}
}
