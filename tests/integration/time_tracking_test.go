package integration

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

type timeEnv struct {
	tc       *TestContext
	owner    workspaceOwner
	member   string
	memberID uint
	guest    string
	outsider string
	taskA    uint
	taskB    uint
}

func newTimeEnv(t *testing.T) *timeEnv {
	t.Helper()
	tc := newTestContext(t)
	e := &timeEnv{tc: tc, owner: newWorkspaceOwner(t, tc, "timetrack")}
	var guestID uint
	e.memberID, e.member = newUser(t, tc, "time-member@example.test")
	guestID, e.guest = newUser(t, tc, "time-guest@example.test")
	_, e.outsider = newUser(t, tc, "time-outsider@example.test")
	for _, u := range []struct {
		id   uint
		role string
	}{{e.memberID, "Member"}, {guestID, "Guest"}} {
		addContact(t, tc, e.owner.token, u.id)
		if rec := request(t, tc.Router, http.MethodPost, membersPath(e.owner.workspaceID, ""), e.owner.token,
			map[string]any{"user_id": u.id, "role": u.role}); rec.Code != http.StatusCreated {
			t.Fatalf("add %s: %d: %s", u.role, rec.Code, rec.Body.String())
		}
	}
	list := mustCreate(t, tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/lists", map[string]any{"name": "L"})
	status := mustCreate(t, tc, e.owner.token, "/api/v1/lists/"+itoa(list)+"/statuses",
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": list})
	task := func(title string) uint {
		return mustCreate(t, tc, e.owner.token, "/api/v1/lists/"+itoa(list)+"/tasks", map[string]any{"title": title, "status_id": status})
	}
	e.taskA, e.taskB = task("A"), task("B")
	return e
}

func timePath(task uint, suffix string) string {
	return "/api/v1/tasks/" + itoa(task) + "/time" + suffix
}

func (e *timeEnv) spent(t *testing.T, task uint) int {
	t.Helper()
	var n int
	if err := e.tc.DB.Raw("SELECT time_spent FROM tasks WHERE id = ?", task).Scan(&n).Error; err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *timeEnv) call(t *testing.T, method, path, token string, body any, want int) map[string]any {
	t.Helper()
	rec := request(t, e.tc.Router, method, path, token, body)
	if rec.Code != want {
		t.Fatalf("%s %s: %d (want %d): %s", method, path, rec.Code, want, rec.Body.String())
	}
	if rec.Code >= 400 {
		return decodeJSON(t, rec)
	}
	return dataObject(t, rec)
}

func TestTimeTracking_ManualEntriesAddUpToTheTaskTime(t *testing.T) {
	e := newTimeEnv(t)
	first := e.call(t, http.MethodPost, timePath(e.taskA, ""), e.member, map[string]any{"seconds": 1800, "note": " fix "}, http.StatusCreated)
	if first["seconds"] != float64(1800) || first["running"] != false || first["note"] == "" {
		t.Fatalf("entry = %v", first)
	}
	if strings.Contains(mustJSON(t, first), "@") {
		t.Fatalf("an entry leaks an e-mail address: %v", first)
	}
	e.call(t, http.MethodPost, timePath(e.taskA, ""), e.owner.token, map[string]any{"seconds": 600}, http.StatusCreated)
	if e.spent(t, e.taskA) != 2400 {
		t.Fatalf("time_spent = %d, want 2400", e.spent(t, e.taskA))
	}
	list := e.call(t, http.MethodGet, timePath(e.taskA, ""), e.guest, nil, http.StatusOK)
	if list["total_seconds"] != float64(2400) || len(list["entries"].([]any)) != 2 {
		t.Fatalf("list = %v", list)
	}

	id := itoa(uint(first["id"].(float64)))
	e.call(t, http.MethodPatch, timePath(e.taskA, "/"+id), e.member, map[string]any{"seconds": 3600, "note": "longer"}, http.StatusOK)
	if e.spent(t, e.taskA) != 4200 {
		t.Fatalf("time_spent after the edit = %d, want 4200", e.spent(t, e.taskA))
	}
	e.call(t, http.MethodDelete, timePath(e.taskA, "/"+id), e.member, nil, http.StatusOK)
	if e.spent(t, e.taskA) != 600 {
		t.Fatalf("time_spent after the delete = %d, want 600", e.spent(t, e.taskA))
	}
}

func TestTimeTracking_TimerStartStopAndOnlyOneAtATime(t *testing.T) {
	e := newTimeEnv(t)
	e.call(t, http.MethodPost, timePath(e.taskA, "/stop"), e.member, nil, http.StatusNotFound)
	started := e.call(t, http.MethodPost, timePath(e.taskA, "/start"), e.member, map[string]any{"note": "work"}, http.StatusCreated)
	if started["running"] != true || started["ended_at"] != nil {
		t.Fatalf("started = %v", started)
	}
	if blocked := e.call(t, http.MethodPost, timePath(e.taskB, "/start"), e.member, nil, http.StatusConflict); !strings.Contains(mustJSON(t, blocked), "TIMER_ALREADY_RUNNING") {
		t.Fatalf("second timer = %v", blocked)
	}
	e.call(t, http.MethodPost, timePath(e.taskB, "/start"), e.owner.token, nil, http.StatusCreated)

	running := e.call(t, http.MethodGet, "/api/v1/time/running", e.member, nil, http.StatusOK)["timer"].(map[string]any)
	if running["task_id"] != float64(e.taskA) || running["task_title"] != "A" {
		t.Fatalf("running = %v", running)
	}
	if none := e.call(t, http.MethodGet, "/api/v1/time/running", e.outsider, nil, http.StatusOK); none["timer"] != nil {
		t.Fatalf("someone with no timer sees %v", none)
	}

	if err := e.tc.DB.Exec("UPDATE task_time_entries SET started_at = now() - interval '90 seconds' WHERE task_id = ? AND user_id = ?", e.taskA, e.memberID).Error; err != nil {
		t.Fatal(err)
	}
	stopped := e.call(t, http.MethodPost, timePath(e.taskA, "/stop"), e.member, nil, http.StatusOK)
	secs, _ := stopped["seconds"].(float64)
	if stopped["running"] != false || secs < 90 || secs > 100 {
		t.Fatalf("stopped = %v", stopped)
	}
	if e.spent(t, e.taskA) != int(secs) {
		t.Fatalf("time_spent = %d, want %v", e.spent(t, e.taskA), secs)
	}
	e.call(t, http.MethodPost, timePath(e.taskB, "/start"), e.member, nil, http.StatusCreated)
	if e.spent(t, e.taskB) != 0 {
		t.Fatal("a running timer must not count into the task time before it stops")
	}
}

func TestTimeTracking_PermissionsAndOwnership(t *testing.T) {
	e := newTimeEnv(t)
	for name, token := range map[string]string{"guest": e.guest, "outsider": e.outsider} {
		e.call(t, http.MethodPost, timePath(e.taskA, ""), token, map[string]any{"seconds": 60}, http.StatusForbidden)
		e.call(t, http.MethodPost, timePath(e.taskA, "/start"), token, nil, http.StatusForbidden)
		_ = name
	}
	e.call(t, http.MethodGet, timePath(e.taskA, ""), e.outsider, nil, http.StatusForbidden)

	entry := e.call(t, http.MethodPost, timePath(e.taskA, ""), e.owner.token, map[string]any{"seconds": 300}, http.StatusCreated)
	id := itoa(uint(entry["id"].(float64)))
	e.call(t, http.MethodPatch, timePath(e.taskA, "/"+id), e.member, map[string]any{"seconds": 1}, http.StatusForbidden)
	e.call(t, http.MethodDelete, timePath(e.taskA, "/"+id), e.member, nil, http.StatusForbidden)

	mine := e.call(t, http.MethodPost, timePath(e.taskA, ""), e.member, map[string]any{"seconds": 120}, http.StatusCreated)
	mineID := itoa(uint(mine["id"].(float64)))
	e.call(t, http.MethodDelete, timePath(e.taskA, "/"+mineID), e.owner.token, nil, http.StatusOK)
	e.call(t, http.MethodDelete, timePath(e.taskA, "/"+mineID), e.owner.token, nil, http.StatusNotFound)

	e.call(t, http.MethodPost, timePath(e.taskA, "/start"), e.member, nil, http.StatusCreated)
	var runningID uint
	if err := e.tc.DB.Raw("SELECT id FROM task_time_entries WHERE task_id = ? AND ended_at IS NULL", e.taskA).Scan(&runningID).Error; err != nil {
		t.Fatal(err)
	}
	e.call(t, http.MethodPatch, timePath(e.taskA, "/"+itoa(runningID)), e.member, map[string]any{"seconds": 60}, http.StatusBadRequest)
	e.call(t, http.MethodPatch, timePath(e.taskA, "/"+itoa(runningID)), e.member, map[string]any{"note": "still going"}, http.StatusOK)
}

func TestTimeTracking_Validation(t *testing.T) {
	e := newTimeEnv(t)
	future := time.Now().Add(2 * time.Hour).UTC().Format(time.RFC3339)
	for name, body := range map[string]map[string]any{
		"zero seconds":    {"seconds": 0},
		"over a day":      {"seconds": 86401},
		"in the future":   {"seconds": 600, "started_at": future},
		"too long a note": {"seconds": 60, "note": strings.Repeat("x", 2001)},
	} {
		e.call(t, http.MethodPost, timePath(e.taskA, ""), e.member, body, http.StatusBadRequest)
		_ = name
	}
	past := time.Now().Add(-5 * time.Hour).UTC().Format(time.RFC3339)
	entry := e.call(t, http.MethodPost, timePath(e.taskA, ""), e.member, map[string]any{"seconds": 600, "started_at": past}, http.StatusCreated)
	if entry["started_at"] == nil || entry["ended_at"] == nil {
		t.Fatalf("entry = %v", entry)
	}
	e.call(t, http.MethodPatch, timePath(e.taskA, "/"+itoa(uint(entry["id"].(float64)))), e.member, map[string]any{}, http.StatusBadRequest)
}

func TestTimeTracking_ReportCountsPerUserAndOnlyManagersSeeEveryone(t *testing.T) {
	e := newTimeEnv(t)
	e.call(t, http.MethodPost, timePath(e.taskA, ""), e.member, map[string]any{"seconds": 1000}, http.StatusCreated)
	e.call(t, http.MethodPost, timePath(e.taskB, ""), e.member, map[string]any{"seconds": 500}, http.StatusCreated)
	e.call(t, http.MethodPost, timePath(e.taskB, ""), e.owner.token, map[string]any{"seconds": 250}, http.StatusCreated)
	e.call(t, http.MethodPost, timePath(e.taskA, "/start"), e.owner.token, nil, http.StatusCreated)
	path := "/api/v1/workspaces/" + itoa(e.owner.workspaceID) + "/time-report"

	all := e.call(t, http.MethodGet, path, e.owner.token, nil, http.StatusOK)
	users := all["users"].([]any)
	if all["total_seconds"] != float64(1750) || len(users) != 2 || users[0].(map[string]any)["seconds"] != float64(1500) {
		t.Fatalf("report = %v", all)
	}
	if strings.Contains(mustJSON(t, all), "@") {
		t.Fatalf("the report leaks an e-mail address: %v", all)
	}
	mine := e.call(t, http.MethodGet, path+"?user_id="+itoa(e.owner.userID), e.member, nil, http.StatusOK)
	if mine["total_seconds"] != float64(1500) || len(mine["users"].([]any)) != 1 {
		t.Fatalf("a member sees only their own row, got %v", mine)
	}
	filtered := e.call(t, http.MethodGet, path+"?user_id="+itoa(e.owner.userID), e.owner.token, nil, http.StatusOK)
	if filtered["total_seconds"] != float64(250) {
		t.Fatalf("filtered = %v", filtered)
	}
	e.call(t, http.MethodGet, path+"?from=2026-02-30", e.owner.token, nil, http.StatusBadRequest)
	e.call(t, http.MethodGet, path+"?from=2020-01-01&to=2026-01-01", e.owner.token, nil, http.StatusBadRequest)
	e.call(t, http.MethodGet, path, e.outsider, nil, http.StatusForbidden)
}

func TestTimeTracking_EntriesGoWithTheirTask(t *testing.T) {
	e := newTimeEnv(t)
	e.call(t, http.MethodPost, timePath(e.taskA, ""), e.member, map[string]any{"seconds": 900}, http.StatusCreated)
	e.call(t, http.MethodDelete, "/api/v1/tasks/"+itoa(e.taskA), e.owner.token, nil, http.StatusOK)
	path := "/api/v1/workspaces/" + itoa(e.owner.workspaceID) + "/time-report"
	if report := e.call(t, http.MethodGet, path, e.owner.token, nil, http.StatusOK); report["total_seconds"] != float64(0) {
		t.Fatalf("the time of a deleted task is still reported: %v", report)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}
