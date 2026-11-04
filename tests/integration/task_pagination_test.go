package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/persistence"
	"rabbit-hole-server/internal/repository"
)

func TestTaskRepositoryCursorNavigationPostgres(t *testing.T) {
	tc, listID, fixtureTasks := setupTaskPaginationFixture(t, 5)
	repo := repository.NewTaskRepository(tc.DB)

	first, hasNext, hasPrev, err := repo.List(context.Background(), listID, domain.TaskQuery{}, pagination.Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, first, fixtureTasks[0:2])
	if !hasNext || hasPrev {
		t.Fatalf("first page flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}

	nextCursor := integrationTaskCursor(t, listID, first[1], pagination.DirectionNext)
	second, hasNext, hasPrev, err := repo.List(context.Background(), listID, domain.TaskQuery{}, pagination.Params{Limit: 2, Cursor: nextCursor})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, second, fixtureTasks[2:4])
	if !hasNext || !hasPrev {
		t.Fatalf("second page flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}

	prevCursor := integrationTaskCursor(t, listID, second[0], pagination.DirectionPrev)
	back, hasNext, hasPrev, err := repo.List(context.Background(), listID, domain.TaskQuery{}, pagination.Params{Limit: 2, Cursor: prevCursor})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, back, fixtureTasks[0:2])
	if !hasNext || hasPrev {
		t.Fatalf("returned first page flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}
}

func TestTaskRepositoryCursorLastPageAndEmptyListPostgres(t *testing.T) {
	tc, listID, fixtureTasks := setupTaskPaginationFixture(t, 3)
	repo := repository.NewTaskRepository(tc.DB)

	first, hasNext, hasPrev, err := repo.List(context.Background(), listID, domain.TaskQuery{}, pagination.Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, first, fixtureTasks[0:2])
	if !hasNext || hasPrev {
		t.Fatalf("first page flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}

	last, hasNext, hasPrev, err := repo.List(context.Background(), listID, domain.TaskQuery{}, pagination.Params{
		Limit:  2,
		Cursor: integrationTaskCursor(t, listID, first[1], pagination.DirectionNext),
	})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, last, fixtureTasks[2:3])
	if hasNext || !hasPrev {
		t.Fatalf("last page flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}

	empty, hasNext, hasPrev, err := repo.List(context.Background(), listID+999999, domain.TaskQuery{}, pagination.Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 || hasNext || hasPrev {
		t.Fatalf("empty list result len=%v flags=(%t,%t)", len(empty), hasNext, hasPrev)
	}
}

func TestTaskRepositoryCursorAfterAnchorDeletionPostgres(t *testing.T) {
	tc, listID, fixtureTasks := setupTaskPaginationFixture(t, 4)
	repo := repository.NewTaskRepository(tc.DB)

	first, _, _, err := repo.List(context.Background(), listID, domain.TaskQuery{}, pagination.Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, first, fixtureTasks[0:2])
	cursor := integrationTaskCursor(t, listID, first[1], pagination.DirectionNext)
	if err := tc.DB.Delete(&persistence.Task{}, first[1].ID).Error; err != nil {
		t.Fatalf("delete cursor anchor: %v", err)
	}

	next, hasNext, hasPrev, err := repo.List(context.Background(), listID, domain.TaskQuery{}, pagination.Params{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, next, fixtureTasks[2:4])
	if hasNext || !hasPrev {
		t.Fatalf("page after anchor deletion flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}
}

func TestTaskRepositoryEmptyPreviousPageDoesNotAdvertiseNextPostgres(t *testing.T) {
	tc, listID, fixtureTasks := setupTaskPaginationFixture(t, 1)
	repo := repository.NewTaskRepository(tc.DB)
	cursor := integrationRawTaskCursor(t, listID, fixtureTasks[0].CreatedAt.Add(time.Hour), 999999, pagination.DirectionPrev)

	items, hasNext, hasPrev, err := repo.List(context.Background(), listID, domain.TaskQuery{}, pagination.Params{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || hasNext || hasPrev {
		t.Fatalf("empty previous page items=%v flags=(%t,%t)", integrationTaskIDsOnly(items), hasNext, hasPrev)
	}
}

func setupTaskPaginationFixture(t *testing.T, count int) (*TestContext, uint, []persistence.Task) {
	t.Helper()
	tc := newTestContext(t)
	name := fmt.Sprintf("pagination-%d", time.Now().UnixNano())
	_, space, list := createWorkspaceTaskGraph(t, tc.DB, name+"@example.test", name)

	var first persistence.Task
	if err := tc.DB.Where("list_id = ?", list.ID).First(&first).Error; err != nil {
		t.Fatalf("find fixture task: %v", err)
	}
	createdAt := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if err := tc.DB.Model(&persistence.Task{}).Where("id = ?", first.ID).Updates(map[string]any{
		"created_at": createdAt,
		"updated_at": createdAt,
	}).Error; err != nil {
		t.Fatalf("set fixture task timestamp: %v", err)
	}

	for i := 1; i < count; i++ {
		task := persistence.Task{
			Model:    gorm.Model{CreatedAt: createdAt, UpdatedAt: createdAt},
			SpaceID:  space.ID,
			ListID:   list.ID,
			StatusID: first.StatusID,
			UserID:   first.UserID,
			Title:    fmt.Sprintf("%s task-%d", name, i+1),
			Priority: 2,
		}
		if err := tc.DB.Create(&task).Error; err != nil {
			t.Fatalf("create fixture task %d: %v", i+1, err)
		}
	}

	var tasks []persistence.Task
	if err := tc.DB.Where("list_id = ?", list.ID).Order("created_at DESC, id DESC").Find(&tasks).Error; err != nil {
		t.Fatalf("load fixture tasks: %v", err)
	}
	if len(tasks) != count {
		t.Fatalf("fixture task count=%d, want %d", len(tasks), count)
	}
	return tc, list.ID, tasks
}

func integrationTaskCursor(t *testing.T, listID uint, task domain.Task, direction pagination.Direction) *pagination.Cursor {
	t.Helper()
	return integrationRawTaskCursor(t, listID, task.CreatedAt, uint64(task.ID), direction)
}

func integrationRawTaskCursor(t *testing.T, listID uint, createdAt time.Time, id uint64, direction pagination.Direction) *pagination.Cursor {
	t.Helper()
	position := pagination.TaskCursorPosition{Keys: []json.RawMessage{json.RawMessage(fmt.Sprint(id))}}
	rawPosition, err := json.Marshal(position)
	if err != nil {
		t.Fatal(err)
	}
	context := domain.TaskQuery{}.CursorContext(listID)
	return &pagination.Cursor{
		Resource:  context.Resource,
		Scope:     context.Scope,
		Sort:      context.Sort,
		Position:  rawPosition,
		Direction: direction,
	}
}

func assertIntegrationTaskIDs(t *testing.T, got []domain.Task, want []persistence.Task) {
	t.Helper()
	gotIDs := integrationTaskIDsOnly(got)
	wantIDs := integrationTaskIDsOnly(want)
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("task IDs=%v, want %v", gotIDs, wantIDs)
	}
}

func integrationTaskIDsOnly(tasks any) []uint {
	switch typed := tasks.(type) {
	case []persistence.Task:
		ids := make([]uint, len(typed))
		for i := range typed {
			ids[i] = typed[i].ID
		}
		return ids
	case []domain.Task:
		ids := make([]uint, len(typed))
		for i := range typed {
			ids[i] = typed[i].ID
		}
		return ids
	default:
		panic("integrationTaskIDsOnly: unsupported type")
	}
}
