package integration

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"testing"
)

type progressEnv struct {
	*moveEnv
}

type progress struct{ total, resolved int }

func newProgressEnv(t *testing.T) *progressEnv {
	return &progressEnv{moveEnv: newMoveEnv(t)}
}

func (e *progressEnv) checklistURL(taskID uint) string {
	return "/api/v1/tasks/" + itoa(taskID) + "/checklists"
}

func (e *progressEnv) addChecklist(t *testing.T, taskID uint) uint {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, e.checklistURL(taskID), e.tc.Token, map[string]any{"name": "steps"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create checklist: %d: %s", rec.Code, rec.Body.String())
	}
	return responseID(t, rec)
}

func (e *progressEnv) addItem(t *testing.T, taskID, checklistID uint, resolved bool) uint {
	t.Helper()
	path := e.checklistURL(taskID) + "/" + itoa(checklistID) + "/items"
	rec := request(t, e.tc.Router, http.MethodPost, path, e.tc.Token, map[string]any{"text": "step"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create item: %d: %s", rec.Code, rec.Body.String())
	}
	id := responseID(t, rec)
	if resolved {
		e.setResolved(t, taskID, checklistID, id, true)
	}
	return id
}

func (e *progressEnv) setResolved(t *testing.T, taskID, checklistID, itemID uint, resolved bool) {
	t.Helper()
	path := e.checklistURL(taskID) + "/" + itoa(checklistID) + "/items/" + itoa(itemID)
	rec := request(t, e.tc.Router, http.MethodPatch, path, e.tc.Token, map[string]any{"resolved": resolved})
	if rec.Code != http.StatusOK {
		t.Fatalf("set resolved: %d: %s", rec.Code, rec.Body.String())
	}
}

func progressOf(t *testing.T, task map[string]any) progress {
	t.Helper()
	total, ok1 := task["checklist_items_total"].(float64)
	resolved, ok2 := task["checklist_items_resolved"].(float64)
	if !ok1 || !ok2 {
		t.Fatalf("task is missing the checklist progress fields: %v", task)
	}
	return progress{int(total), int(resolved)}
}

func (e *progressEnv) viaGet(t *testing.T, taskID uint) progress {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID), e.tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d: %s", rec.Code, rec.Body.String())
	}
	return progressOf(t, dataObject(t, rec))
}

func (e *progressEnv) viaList(t *testing.T, listID, taskID uint) progress {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/lists/"+itoa(listID)+"/tasks?limit=100", e.tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tasks: %d: %s", rec.Code, rec.Body.String())
	}
	for _, raw := range dataObject(t, rec)["items"].([]any) {
		task := raw.(map[string]any)
		if uint(task["id"].(float64)) == taskID {
			return progressOf(t, task)
		}
	}
	t.Fatalf("task %d missing from the list", taskID)
	return progress{}
}

func (e *progressEnv) viaDashboard(t *testing.T, taskID uint) progress {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(e.spaceID)+"/dashboard", e.tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard: %d: %s", rec.Code, rec.Body.String())
	}
	for _, l := range dataObject(t, rec)["lists"].([]any) {
		list := l.(map[string]any)
		id, ok := list["id"]
		if !ok {
			id = list["ID"]
		}
		if uint(id.(float64)) != e.listID {
			continue
		}
		for _, raw := range list["tasks"].([]any) {
			task := raw.(map[string]any)
			if uint(task["id"].(float64)) == taskID {
				return progressOf(t, task)
			}
		}
	}
	t.Fatalf("task %d missing from the dashboard", taskID)
	return progress{}
}

func (e *progressEnv) expect(t *testing.T, step string, taskID uint, want progress) {
	t.Helper()
	if got := e.viaGet(t, taskID); got != want {
		t.Fatalf("%s: GET /tasks/{id} = %+v, want %+v", step, got, want)
	}
	if got := e.viaList(t, e.listID, taskID); got != want {
		t.Fatalf("%s: list = %+v, want %+v", step, got, want)
	}
	if got := e.viaDashboard(t, taskID); got != want {
		t.Fatalf("%s: dashboard = %+v, want %+v", step, got, want)
	}
}

func TestTaskChecklistProgress_FollowsEveryChecklistChange(t *testing.T) {
	e := newProgressEnv(t)
	taskID := e.task(t, e.listID, e.statusA, "progress task")
	e.expect(t, "no checklists", taskID, progress{})

	rec := request(t, e.tc.Router, http.MethodPost, e.tasksURL, e.tc.Token, map[string]any{"title": "fresh task", "status_id": e.statusA})
	if got := progressOf(t, dataObject(t, rec)); got != (progress{}) {
		t.Fatalf("create response = %+v, want zeros", got)
	}

	first := e.addChecklist(t, taskID)
	e.expect(t, "empty checklist", taskID, progress{})

	a := e.addItem(t, taskID, first, false)
	b := e.addItem(t, taskID, first, false)
	e.addItem(t, taskID, first, false)
	e.expect(t, "three open items", taskID, progress{3, 0})

	e.setResolved(t, taskID, first, a, true)
	e.expect(t, "one resolved", taskID, progress{3, 1})
	e.setResolved(t, taskID, first, a, true)
	e.expect(t, "resolved twice", taskID, progress{3, 1})
	e.setResolved(t, taskID, first, b, true)
	e.expect(t, "two resolved", taskID, progress{3, 2})
	e.setResolved(t, taskID, first, a, false)
	e.expect(t, "reopened", taskID, progress{3, 1})

	second := e.addChecklist(t, taskID)
	e.addItem(t, taskID, second, true)
	e.addItem(t, taskID, second, false)
	e.expect(t, "two checklists", taskID, progress{5, 2})

	itemPath := e.checklistURL(taskID) + "/" + itoa(first) + "/items/" + itoa(b)
	if rec := request(t, e.tc.Router, http.MethodDelete, itemPath, e.tc.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete item: %d", rec.Code)
	}
	e.expect(t, "resolved item deleted", taskID, progress{4, 1})

	if rec := request(t, e.tc.Router, http.MethodDelete, e.checklistURL(taskID)+"/"+itoa(first), e.tc.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete checklist: %d", rec.Code)
	}
	e.expect(t, "first checklist deleted", taskID, progress{2, 1})

	if rec := request(t, e.tc.Router, http.MethodDelete, e.checklistURL(taskID)+"/"+itoa(second), e.tc.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete checklist: %d", rec.Code)
	}
	e.expect(t, "all checklists deleted", taskID, progress{})
}

func TestTaskChecklistProgress_UpdateMoveAndSubtasks(t *testing.T) {
	e := newProgressEnv(t)
	otherList := createList(t, e.tc, e.spaceID)
	otherStatus := e.status(t, otherList, "C")

	parent := e.task(t, e.listID, e.statusA, "parent task")
	check := e.addChecklist(t, parent)
	e.addItem(t, parent, check, true)
	e.addItem(t, parent, check, false)

	rec := request(t, e.tc.Router, http.MethodPost, e.tasksURL, e.tc.Token,
		map[string]any{"title": "child task", "status_id": e.statusA, "parent_id": parent})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create subtask: %d: %s", rec.Code, rec.Body.String())
	}
	child := responseID(t, rec)
	childCheck := e.addChecklist(t, child)
	for i := 0; i < 3; i++ {
		e.addItem(t, child, childCheck, true)
	}

	e.expect(t, "parent ignores its subtask", parent, progress{2, 1})
	e.expect(t, "subtask has its own", child, progress{3, 3})

	rec = request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(parent), e.tc.Token, map[string]any{"title": "renamed parent", "priority": 4})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d: %s", rec.Code, rec.Body.String())
	}
	if got := progressOf(t, dataObject(t, rec)); got != (progress{2, 1}) {
		t.Fatalf("update response = %+v", got)
	}
	e.expect(t, "after update", parent, progress{2, 1})

	rec = request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(child), e.tc.Token, map[string]any{"parent_id": nil})
	if rec.Code != http.StatusOK {
		t.Fatalf("detach subtask: %d: %s", rec.Code, rec.Body.String())
	}
	if got := progressOf(t, dataObject(t, rec)); got != (progress{3, 3}) {
		t.Fatalf("detach response = %+v", got)
	}

	rec = e.move(t, child, map[string]any{"status_id": e.statusB})
	if rec.Code != http.StatusOK {
		t.Fatalf("move within list: %d: %s", rec.Code, rec.Body.String())
	}
	if got := progressOf(t, dataObject(t, rec)); got != (progress{3, 3}) {
		t.Fatalf("move response = %+v", got)
	}
	rec = e.move(t, child, map[string]any{"list_id": otherList, "status_id": otherStatus})
	if rec.Code != http.StatusOK {
		t.Fatalf("move to another list: %d: %s", rec.Code, rec.Body.String())
	}
	if got := progressOf(t, dataObject(t, rec)); got != (progress{3, 3}) {
		t.Fatalf("cross-list move response = %+v", got)
	}
	if got := e.viaList(t, otherList, child); got != (progress{3, 3}) {
		t.Fatalf("moved task in its new list = %+v", got)
	}
}

func TestTaskChecklistProgress_PaginationSortingAndFilter(t *testing.T) {
	e := newProgressEnv(t)
	want := map[uint]progress{}
	var ids []uint
	shapes := []progress{{0, 0}, {3, 1}, {2, 2}, {4, 0}, {0, 0}, {5, 3}, {1, 0}, {3, 3}, {2, 0}}
	for i, shape := range shapes {
		id := e.task(t, e.listID, e.statusA, fmt.Sprintf("task number %d", i))
		if shape.total > 0 {
			check := e.addChecklist(t, id)
			if i%2 == 0 {
				e.addChecklist(t, id)
			}
			for n := 0; n < shape.total; n++ {
				e.addItem(t, id, check, n < shape.resolved)
			}
		}
		want[id] = shape
		ids = append(ids, id)
	}
	if rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(ids[0]), e.tc.Token, map[string]any{"priority": 4}); rec.Code != http.StatusOK {
		t.Fatalf("set priority: %d", rec.Code)
	}

	walk := func(query url.Values, limit int) ([]uint, int) {
		t.Helper()
		var seen []uint
		cursor, pages := "", 0
		for {
			q := url.Values{}
			for k, v := range query {
				q[k] = v
			}
			q.Set("limit", fmt.Sprint(limit))
			if cursor != "" {
				q.Set("cursor", cursor)
			}
			rec := request(t, e.tc.Router, http.MethodGet, e.tasksURL+"?"+q.Encode(), e.tc.Token, nil)
			if rec.Code != http.StatusOK {
				t.Fatalf("list %v: %d: %s", q, rec.Code, rec.Body.String())
			}
			data := dataObject(t, rec)
			pages++
			for _, raw := range data["items"].([]any) {
				task := raw.(map[string]any)
				id := uint(task["id"].(float64))
				if got := progressOf(t, task); got != want[id] {
					t.Fatalf("task %d on page %d = %+v, want %+v", id, pages, got, want[id])
				}
				seen = append(seen, id)
			}
			next, _ := data["next_cursor"].(string)
			if next == "" {
				return seen, pages
			}
			cursor = next
			if pages > 20 {
				t.Fatal("pagination does not terminate")
			}
		}
	}

	for _, sortBy := range []string{"", "-priority,title", "status,-id", "priority,-created_at"} {
		query := url.Values{}
		if sortBy != "" {
			query.Set("sort", sortBy)
		}
		single, _ := walk(query, 100)
		paged, pages := walk(query, 2)
		if pages < 4 {
			t.Fatalf("sort %q: only %d pages", sortBy, pages)
		}
		if fmt.Sprint(single) != fmt.Sprint(paged) || len(paged) != len(ids) {
			t.Fatalf("sort %q: paged %v differs from single page %v", sortBy, paged, single)
		}
	}

	for _, open := range []bool{true, false} {
		var expected []uint
		for _, id := range ids {
			if (want[id].total > want[id].resolved) == open {
				expected = append(expected, id)
			}
		}
		query := url.Values{"has_open_checklist_items": {fmt.Sprint(open)}, "with_total": {"true"}}
		got, pages := walk(query, 2)
		if pages < 2 {
			t.Fatalf("open=%v: expected several pages", open)
		}
		sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
		if fmt.Sprint(got) != fmt.Sprint(expected) {
			t.Fatalf("open=%v: got %v, want %v", open, got, expected)
		}
		rec := request(t, e.tc.Router, http.MethodGet, e.tasksURL+"?limit=1&"+query.Encode(), e.tc.Token, nil)
		if total := dataObject(t, rec)["total_count"]; total != float64(len(expected)) {
			t.Fatalf("open=%v: total_count = %v, want %d", open, total, len(expected))
		}
	}

	combined := url.Values{"has_open_checklist_items": {"true"}, "priority": {"2"}, "sort": {"-priority,title"}}
	got, _ := walk(combined, 2)
	if len(got) != 5 {
		t.Fatalf("combined filter returned %v", got)
	}

	rec := request(t, e.tc.Router, http.MethodGet, e.tasksURL+"?limit=2&has_open_checklist_items=true", e.tc.Token, nil)
	next := dataObject(t, rec)["next_cursor"].(string)
	rec = request(t, e.tc.Router, http.MethodGet, e.tasksURL+"?limit=2&has_open_checklist_items=false&cursor="+url.QueryEscape(next), e.tc.Token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cursor reused with another checklist filter: expected 400, got %d", rec.Code)
	}
	rec = request(t, e.tc.Router, http.MethodGet, e.tasksURL+"?has_open_checklist_items=maybe", e.tc.Token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid has_open_checklist_items: expected 400, got %d", rec.Code)
	}
}
