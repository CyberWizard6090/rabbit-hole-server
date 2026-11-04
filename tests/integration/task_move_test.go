package integration

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

type moveEnv struct {
	tc       *TestContext
	spaceID  uint
	listID   uint
	statusA  uint
	statusB  uint
	tasksURL string
}

func newMoveEnv(t *testing.T) *moveEnv {
	t.Helper()
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)
	env := &moveEnv{tc: tc, spaceID: spaceID, listID: listID, tasksURL: "/api/v1/lists/" + itoa(listID) + "/tasks"}
	env.statusA = env.status(t, listID, "A")
	env.statusB = env.status(t, listID, "B")
	return env
}

func (e *moveEnv) status(t *testing.T, listID uint, name string) uint {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", e.tc.Token,
		map[string]any{"name": name, "color": "#112233", "type": 1, "list_id": listID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func (e *moveEnv) task(t *testing.T, listID, statusID uint, title string) uint {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", e.tc.Token,
		map[string]any{"title": title, "status_id": statusID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task %q: %d: %s", title, rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func (e *moveEnv) move(t *testing.T, id uint, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, e.tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(id)+"/move", e.tc.Token, body)
}

func (e *moveEnv) mustMove(t *testing.T, id uint, body map[string]any) {
	t.Helper()
	if rec := e.move(t, id, body); rec.Code != http.StatusOK {
		t.Fatalf("move %d %v: %d: %s", id, body, rec.Code, rec.Body.String())
	}
}

func (e *moveEnv) order(t *testing.T, listID uint) []uint {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/lists/"+itoa(listID)+"/tasks?sort=position&limit=100", e.tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks: %d: %s", rec.Code, rec.Body.String())
	}
	items, _ := decodeJSON(t, rec)["data"].(map[string]any)["items"].([]any)
	ids := make([]uint, 0, len(items))
	for _, raw := range items {
		ids = append(ids, uint(raw.(map[string]any)["id"].(float64)))
	}
	return ids
}

func TestTaskMove_ReordersWithinListAndAcrossStatuses(t *testing.T) {
	env := newMoveEnv(t)
	a := env.task(t, env.listID, env.statusA, "first task")
	b := env.task(t, env.listID, env.statusA, "second task")
	c := env.task(t, env.listID, env.statusA, "third task")

	if got, want := env.order(t, env.listID), []uint{a, b, c}; !reflect.DeepEqual(got, want) {
		t.Fatalf("new tasks are appended: got %v, want %v", got, want)
	}

	env.mustMove(t, c, map[string]any{"before_id": a})
	if got, want := env.order(t, env.listID), []uint{c, a, b}; !reflect.DeepEqual(got, want) {
		t.Fatalf("before first: got %v, want %v", got, want)
	}

	env.mustMove(t, c, map[string]any{"after_id": a})
	if got, want := env.order(t, env.listID), []uint{a, c, b}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after anchor: got %v, want %v", got, want)
	}

	env.mustMove(t, a, map[string]any{})
	if got, want := env.order(t, env.listID), []uint{c, b, a}; !reflect.DeepEqual(got, want) {
		t.Fatalf("no anchor moves to the end: got %v, want %v", got, want)
	}

	env.mustMove(t, b, map[string]any{"status_id": env.statusB})
	var statusID uint
	if err := env.tc.DB.Table("tasks").Select("status_id").Where("id = ?", b).Scan(&statusID).Error; err != nil {
		t.Fatalf("read status: %v", err)
	}
	if statusID != env.statusB {
		t.Fatalf("status_id = %d, want %d", statusID, env.statusB)
	}
}

func TestTaskMove_RebalancesWhenTheGapIsUsedUp(t *testing.T) {
	env := newMoveEnv(t)
	a := env.task(t, env.listID, env.statusA, "anchor first")
	b := env.task(t, env.listID, env.statusA, "anchor last")
	x := env.task(t, env.listID, env.statusA, "mover x")
	y := env.task(t, env.listID, env.statusA, "mover y")

	var last uint
	for i := 0; i < 40; i++ {
		last = x
		if i%2 == 1 {
			last = y
		}
		env.mustMove(t, last, map[string]any{"after_id": a})
	}

	other := x
	if last == x {
		other = y
	}
	if got, want := env.order(t, env.listID), []uint{a, last, other, b}; !reflect.DeepEqual(got, want) {
		t.Fatalf("order after many inserts: got %v, want %v", got, want)
	}
	var distinct int64
	env.tc.DB.Raw("SELECT count(DISTINCT position) FROM tasks WHERE list_id = ?", env.listID).Scan(&distinct)
	if distinct != 4 {
		t.Fatalf("distinct positions = %d, want 4", distinct)
	}
}

func TestTaskMove_ToAnotherListAndSpace(t *testing.T) {
	env := newMoveEnv(t)
	otherList := createList(t, env.tc, env.spaceID)
	otherStatus := env.status(t, otherList, "Other")
	existing := env.task(t, otherList, otherStatus, "already there")

	parent := env.task(t, env.listID, env.statusA, "parent task")
	rec := request(t, env.tc.Router, http.MethodPost, env.tasksURL, env.tc.Token,
		map[string]any{"title": "child task", "status_id": env.statusA, "parent_id": parent})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create child: %d: %s", rec.Code, rec.Body.String())
	}
	child := responseID(t, rec)

	if rec := env.move(t, parent, map[string]any{"list_id": otherList, "status_id": otherStatus}); rec.Code != http.StatusConflict {
		t.Fatalf("task with subtasks: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := env.move(t, child, map[string]any{"list_id": otherList}); rec.Code != http.StatusBadRequest {
		t.Fatalf("other list without status: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := env.move(t, child, map[string]any{"list_id": otherList, "status_id": env.statusA}); rec.Code != http.StatusBadRequest {
		t.Fatalf("status of another list: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := env.move(t, child, map[string]any{"list_id": otherList, "status_id": otherStatus, "after_id": parent}); rec.Code != http.StatusBadRequest {
		t.Fatalf("anchor from the source list: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	env.mustMove(t, child, map[string]any{"list_id": otherList, "status_id": otherStatus, "before_id": existing})
	if got, want := env.order(t, otherList), []uint{child, existing}; !reflect.DeepEqual(got, want) {
		t.Fatalf("target list order: got %v, want %v", got, want)
	}
	var parentID *uint
	env.tc.DB.Table("tasks").Select("parent_id").Where("id = ?", child).Scan(&parentID)
	if parentID != nil {
		t.Fatalf("parent_id = %v after changing list, want NULL", *parentID)
	}
	if got := env.order(t, env.listID); !reflect.DeepEqual(got, []uint{parent}) {
		t.Fatalf("source list = %v, want only the parent", got)
	}
}

func TestTaskMove_AcrossSpacesDropsTagsAndRespectsWorkspace(t *testing.T) {
	env := newMoveEnv(t)
	task := env.task(t, env.listID, env.statusA, "tagged task")
	rec := request(t, env.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(task), env.tc.Token,
		map[string]any{"add_tag_names": []string{"urgent"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("tag task: %d: %s", rec.Code, rec.Body.String())
	}

	var workspaceID uint
	env.tc.DB.Table("spaces").Select("workspace_id").Where("id = ?", env.spaceID).Scan(&workspaceID)
	rec = request(t, env.tc.Router, http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%d/spaces", workspaceID), env.tc.Token,
		map[string]any{"name": "second space"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: %d: %s", rec.Code, rec.Body.String())
	}
	secondSpace := responseID(t, rec)
	secondList := createList(t, env.tc, secondSpace)
	secondStatus := env.status(t, secondList, "Todo")

	foreignSpace := createSpaceIn(t, env.tc)
	foreignList := createList(t, env.tc, foreignSpace)
	foreignStatus := env.status(t, foreignList, "Todo")
	if rec := env.move(t, task, map[string]any{"list_id": foreignList, "status_id": foreignStatus}); rec.Code != http.StatusBadRequest {
		t.Fatalf("list of another workspace: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	env.mustMove(t, task, map[string]any{"list_id": secondList, "status_id": secondStatus})
	var gotSpace uint
	env.tc.DB.Table("tasks").Select("space_id").Where("id = ?", task).Scan(&gotSpace)
	if gotSpace != secondSpace {
		t.Fatalf("space_id = %d, want %d", gotSpace, secondSpace)
	}
	var tagLinks int64
	env.tc.DB.Table("task_tags").Where("task_id = ?", task).Count(&tagLinks)
	if tagLinks != 0 {
		t.Fatalf("task_tags = %d after changing space, want 0", tagLinks)
	}
}
