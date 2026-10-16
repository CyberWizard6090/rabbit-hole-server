package integration

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
	"rabbit-hole-server/internal/repository"
)

func TestTaskRepositoryCursorNavigationPostgres(t *testing.T) {
	tc, listID, fixtureTasks := setupTaskPaginationFixture(t, 5)
	repo := repository.NewTaskRepository(tc.DB)

	first, hasNext, hasPrev, err := repo.List(listID, pagination.Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, first, fixtureTasks[0:2])
	if !hasNext || hasPrev {
		t.Fatalf("first page flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}

	nextCursor := integrationTaskCursor(t, listID, first[1], pagination.DirectionNext)
	second, hasNext, hasPrev, err := repo.List(listID, pagination.Params{Limit: 2, Cursor: nextCursor})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, second, fixtureTasks[2:4])
	if !hasNext || !hasPrev {
		t.Fatalf("second page flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}

	prevCursor := integrationTaskCursor(t, listID, second[0], pagination.DirectionPrev)
	back, hasNext, hasPrev, err := repo.List(listID, pagination.Params{Limit: 2, Cursor: prevCursor})
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

	first, hasNext, hasPrev, err := repo.List(listID, pagination.Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, first, fixtureTasks[0:2])
	if !hasNext || hasPrev {
		t.Fatalf("first page flags: hasNext=%t hasPrev=%t", hasNext, hasPrev)
	}

	last, hasNext, hasPrev, err := repo.List(listID, pagination.Params{
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

	empty, hasNext, hasPrev, err := repo.List(listID+999999, pagination.Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 || hasNext || hasPrev {
		t.Fatalf("empty list result=%v flags=(%t,%t)", integrationTaskIDsOnly(empty), hasNext, hasPrev)
	}
}

func TestTaskRepositoryCursorAfterAnchorDeletionPostgres(t *testing.T) {
	tc, listID, fixtureTasks := setupTaskPaginationFixture(t, 4)
	repo := repository.NewTaskRepository(tc.DB)

	first, _, _, err := repo.List(listID, pagination.Params{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	assertIntegrationTaskIDs(t, first, fixtureTasks[0:2])
	cursor := integrationTaskCursor(t, listID, first[1], pagination.DirectionNext)
	if err := tc.DB.Delete(&domain.Task{}, first[1].ID).Error; err != nil {
		t.Fatalf("delete cursor anchor: %v", err)
	}

	next, hasNext, hasPrev, err := repo.List(listID, pagination.Params{Limit: 2, Cursor: cursor})
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

	items, hasNext, hasPrev, err := repo.List(listID, pagination.Params{Limit: 2, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 || hasNext || hasPrev {
		t.Fatalf("empty previous page items=%v flags=(%t,%t)", integrationTaskIDsOnly(items), hasNext, hasPrev)
	}
}

func setupTaskPaginationFixture(t *testing.T, count int) (*TestContext, uint, []domain.Task) {
	t.Helper()
	tc := newTestContext(t)
	name := fmt.Sprintf("pagination-%d", time.Now().UnixNano())
	_, space, list := createWorkspaceTaskGraph(t, tc.DB, name+"@example.test", name)

	var first domain.Task
	if err := tc.DB.Where("list_id = ?", list.ID).First(&first).Error; err != nil {
		t.Fatalf("find fixture task: %v", err)
	}
	createdAt := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	if err := tc.DB.Model(&domain.Task{}).Where("id = ?", first.ID).Updates(map[string]any{
		"created_at": createdAt,
		"updated_at": createdAt,
	}).Error; err != nil {
		t.Fatalf("set fixture task timestamp: %v", err)
	}

	for i := 1; i < count; i++ {
		task := domain.Task{
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

	var tasks []domain.Task
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
	position := pagination.TaskCursorPosition{CreatedAt: createdAt, ID: id}
	rawPosition, err := json.Marshal(position)
	if err != nil {
		t.Fatal(err)
	}
	context := pagination.TaskCursorContext(listID)
	return &pagination.Cursor{
		Resource:  context.Resource,
		Scope:     context.Scope,
		Sort:      context.Sort,
		Position:  rawPosition,
		Direction: direction,
	}
}

func assertIntegrationTaskIDs(t *testing.T, got, want []domain.Task) {
	t.Helper()
	if !reflect.DeepEqual(integrationTaskIDsOnly(got), integrationTaskIDsOnly(want)) {
		t.Fatalf("task IDs=%v, want %v", integrationTaskIDsOnly(got), integrationTaskIDsOnly(want))
	}
}

func integrationTaskIDsOnly(tasks []domain.Task) []uint {
	ids := make([]uint, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].ID
	}
	return ids
}
