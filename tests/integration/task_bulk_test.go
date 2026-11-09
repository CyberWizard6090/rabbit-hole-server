package integration

import (
	"net/http"
	"strings"
	"testing"
)

type bulkEnv struct {
	tc          *TestContext
	owner       workspaceOwner
	guestToken  string
	outsider    string
	listA       uint
	listB       uint
	statusA     []uint
	statusB     uint
	tasks       []uint
	foreignTask uint
}

func newBulkEnv(t *testing.T) *bulkEnv {
	t.Helper()
	tc := newTestContext(t)
	e := &bulkEnv{tc: tc, owner: newWorkspaceOwner(t, tc, "bulk")}
	guestID, guestToken := newUser(t, tc, "bulk-guest@example.test")
	_, outsiderToken := newUser(t, tc, "bulk-outsider@example.test")
	addContact(t, tc, e.owner.token, guestID)
	if rec := request(t, tc.Router, http.MethodPost, membersPath(e.owner.workspaceID, ""), e.owner.token,
		map[string]any{"user_id": guestID, "role": "Guest"}); rec.Code != http.StatusCreated {
		t.Fatalf("add guest: %d: %s", rec.Code, rec.Body.String())
	}
	e.guestToken, e.outsider = guestToken, outsiderToken
	spaceLists := "/api/v1/spaces/" + itoa(e.owner.spaceID) + "/lists"
	e.listA = mustCreate(t, tc, e.owner.token, spaceLists, map[string]any{"name": "A"})
	e.listB = mustCreate(t, tc, e.owner.token, spaceLists, map[string]any{"name": "B"})
	status := func(list uint, name string) uint {
		return mustCreate(t, tc, e.owner.token, "/api/v1/lists/"+itoa(list)+"/statuses",
			map[string]any{"name": name, "color": "#112233", "type": 1, "list_id": list})
	}
	e.statusA = []uint{status(e.listA, "Todo"), status(e.listA, "Doing")}
	e.statusB = status(e.listB, "Other")
	for i := 0; i < 4; i++ {
		e.tasks = append(e.tasks, mustCreate(t, tc, e.owner.token, "/api/v1/lists/"+itoa(e.listA)+"/tasks",
			map[string]any{"title": "bulk " + itoa(uint(i)), "status_id": e.statusA[0], "priority": 2}))
	}
	foreign := newWorkspaceOwner(t, tc, "bulkforeign")
	flist := mustCreate(t, tc, foreign.token, "/api/v1/spaces/"+itoa(foreign.spaceID)+"/lists", map[string]any{"name": "F"})
	fstatus := mustCreate(t, tc, foreign.token, "/api/v1/lists/"+itoa(flist)+"/statuses",
		map[string]any{"name": "F", "color": "#112233", "type": 1, "list_id": flist})
	e.foreignTask = mustCreate(t, tc, foreign.token, "/api/v1/lists/"+itoa(flist)+"/tasks",
		map[string]any{"title": "someone else's", "status_id": fstatus})
	return e
}

func (e *bulkEnv) bulk(t *testing.T, token string, body map[string]any) (int, map[string]any) {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/tasks/bulk", token, body)
	if rec.Code != http.StatusOK {
		return rec.Code, decodeJSON(t, rec)
	}
	return rec.Code, dataObject(t, rec)
}

func bulkResults(t *testing.T, data map[string]any) map[uint]map[string]any {
	t.Helper()
	out := map[uint]map[string]any{}
	for _, item := range data["results"].([]any) {
		m := item.(map[string]any)
		out[uint(m["task_id"].(float64))] = m
	}
	return out
}

func (e *bulkEnv) priority(t *testing.T, id uint) int {
	t.Helper()
	var p int
	if err := e.tc.DB.Raw("SELECT priority FROM tasks WHERE id = ?", id).Scan(&p).Error; err != nil {
		t.Fatal(err)
	}
	return p
}

func TestBulkTasks_UpdateChangesEveryTaskAndKeepsTheHistory(t *testing.T) {
	e := newBulkEnv(t)
	code, data := e.bulk(t, e.owner.token, map[string]any{
		"task_ids": []uint{e.tasks[0], e.tasks[1], e.tasks[2], e.tasks[0]}, "action": "update",
		"update": map[string]any{"priority": 5, "status_id": e.statusA[1], "assignee_ids": []uint{e.owner.userID}},
	})
	if code != http.StatusOK || data["succeeded"].(float64) != 3 || data["failed"].(float64) != 0 {
		t.Fatalf("%d %v", code, data)
	}
	for _, id := range e.tasks[:3] {
		if e.priority(t, id) != 5 {
			t.Fatalf("task %d priority = %d", id, e.priority(t, id))
		}
	}
	if e.priority(t, e.tasks[3]) != 2 {
		t.Fatal("a task that was not listed changed")
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM task_activities WHERE task_id IN (?, ?, ?) AND action = 'task.updated' AND field = 'priority'",
		e.tasks[0], e.tasks[1], e.tasks[2]); n != 3 {
		t.Fatalf("priority history rows = %d, want one per task", n)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM task_assignees WHERE user_id = ? AND task_id IN (?, ?, ?)", e.owner.userID, e.tasks[0], e.tasks[1], e.tasks[2]); n != 3 {
		t.Fatalf("assignees set on %d tasks, want 3", n)
	}
}

func TestBulkTasks_PartialSuccessReportsEachTask(t *testing.T) {
	e := newBulkEnv(t)
	code, data := e.bulk(t, e.owner.token, map[string]any{
		"task_ids": []uint{e.tasks[0], e.foreignTask, 99999999, e.tasks[1]}, "action": "update",
		"update": map[string]any{"priority": 4},
	})
	if code != http.StatusOK || data["succeeded"].(float64) != 2 || data["failed"].(float64) != 2 {
		t.Fatalf("%d %v", code, data)
	}
	results := bulkResults(t, data)
	for _, id := range []uint{e.foreignTask, 99999999} {
		if results[id]["ok"] != false || results[id]["code"] != "FORBIDDEN" {
			t.Fatalf("task %d = %v, want FORBIDDEN (a foreign and a missing task look the same)", id, results[id])
		}
	}
	if e.priority(t, e.foreignTask) == 4 {
		t.Fatal("a task of another workspace was changed")
	}

	_, data = e.bulk(t, e.owner.token, map[string]any{
		"task_ids": []uint{e.tasks[2]}, "action": "update", "update": map[string]any{"status_id": e.statusB},
	})
	if r := bulkResults(t, data)[e.tasks[2]]; r["ok"] != false || r["code"] != "INVALID_TASK_REFERENCE" {
		t.Fatalf("a status of another list = %v", r)
	}
}

func TestBulkTasks_MoveAndDelete(t *testing.T) {
	e := newBulkEnv(t)
	_, data := e.bulk(t, e.owner.token, map[string]any{
		"task_ids": []uint{e.tasks[0], e.tasks[1]}, "action": "move", "move": map[string]any{"list_id": e.listB, "status_id": e.statusB},
	})
	if data["succeeded"].(float64) != 2 {
		t.Fatalf("move: %v", data)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE list_id = ? AND status_id = ?", e.listB, e.statusB); n != 2 {
		t.Fatalf("tasks in the target list = %d", n)
	}
	_, data = e.bulk(t, e.owner.token, map[string]any{"task_ids": []uint{e.tasks[2], e.tasks[3]}, "action": "delete"})
	if data["succeeded"].(float64) != 2 {
		t.Fatalf("delete: %v", data)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE id IN (?, ?) AND deleted_at IS NULL", e.tasks[2], e.tasks[3]); n != 0 {
		t.Fatalf("%d deleted tasks are still live", n)
	}
}

func TestBulkTasks_PermissionsAreCheckedForEveryTask(t *testing.T) {
	e := newBulkEnv(t)
	for name, token := range map[string]string{"guest": e.guestToken, "outsider": e.outsider} {
		_, data := e.bulk(t, token, map[string]any{
			"task_ids": []uint{e.tasks[0]}, "action": "update", "update": map[string]any{"priority": 1},
		})
		if r := bulkResults(t, data)[e.tasks[0]]; r["ok"] != false || r["code"] != "FORBIDDEN" {
			t.Fatalf("%s: %v", name, r)
		}
		_, data = e.bulk(t, token, map[string]any{"task_ids": []uint{e.tasks[0]}, "action": "delete"})
		if r := bulkResults(t, data)[e.tasks[0]]; r["ok"] != false {
			t.Fatalf("%s deleted a task: %v", name, r)
		}
	}
	if e.priority(t, e.tasks[0]) != 2 {
		t.Fatal("a task was changed without permission")
	}
	if code, _ := e.bulk(t, "", map[string]any{"task_ids": []uint{1}, "action": "delete"}); code != http.StatusUnauthorized {
		t.Fatalf("anonymous bulk: %d", code)
	}
}

func TestBulkTasks_RequestValidation(t *testing.T) {
	e := newBulkEnv(t)
	many := make([]uint, 101)
	for i := range many {
		many[i] = uint(i + 1)
	}
	cases := map[string]map[string]any{
		"no ids":           {"task_ids": []uint{}, "action": "delete"},
		"too many ids":     {"task_ids": many, "action": "delete"},
		"unknown action":   {"task_ids": []uint{1}, "action": "archive"},
		"update without":   {"task_ids": []uint{1}, "action": "update"},
		"update empty":     {"task_ids": []uint{1}, "action": "update", "update": map[string]any{}},
		"move without":     {"task_ids": []uint{1}, "action": "move"},
		"move and update":  {"task_ids": []uint{1}, "action": "move", "move": map[string]any{"list_id": 1}, "update": map[string]any{"priority": 1}},
		"delete with body": {"task_ids": []uint{1}, "action": "delete", "update": map[string]any{"priority": 1}},
		"bad priority":     {"task_ids": []uint{1}, "action": "update", "update": map[string]any{"priority": 9}},
		"zero id":          {"task_ids": []uint{0}, "action": "delete"},
	}
	for name, body := range cases {
		code, resp := e.bulk(t, e.owner.token, body)
		if code != http.StatusBadRequest {
			t.Errorf("%s: %d: %v", name, code, resp)
		}
	}
	if code, _ := e.bulk(t, e.owner.token, map[string]any{"task_ids": []uint{1}, "action": "delete", "extra": strings.Repeat("x", 5)}); code == http.StatusInternalServerError {
		t.Fatal("an unknown field must not crash the handler")
	}
}
