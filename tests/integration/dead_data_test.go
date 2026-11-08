package integration

import (
	"fmt"
	"net/http"
	"testing"
)

type deadEnv struct {
	tc       *TestContext
	owner    workspaceOwner
	folderID uint
	listID   uint
	statusID uint
	parentID uint
	childID  uint
	taskID   uint
}

func newDeadEnv(t *testing.T) *deadEnv {
	t.Helper()
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "dead")
	e := &deadEnv{tc: tc, owner: owner}
	e.folderID = mustCreate(t, tc, owner.token, "/api/v1/spaces/"+itoa(owner.spaceID)+"/folders", map[string]any{"name": "Folder"})
	e.listID = mustCreate(t, tc, owner.token, "/api/v1/folders/"+itoa(e.folderID)+"/lists", map[string]any{"name": "List"})
	e.statusID = mustCreate(t, tc, owner.token, "/api/v1/lists/"+itoa(e.listID)+"/statuses",
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": e.listID})
	e.parentID = e.task(t, "parent task", nil)
	parent := e.parentID
	e.childID = e.task(t, "child task", &parent)
	e.taskID = e.task(t, "plain task", nil)
	return e
}

func (e *deadEnv) task(t *testing.T, title string, parent *uint) uint {
	t.Helper()
	body := map[string]any{"title": title, "status_id": e.statusID}
	if parent != nil {
		body["parent_id"] = *parent
	}
	return mustCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(e.listID)+"/tasks", body)
}

func (e *deadEnv) do(t *testing.T, method, path string, body any) int {
	t.Helper()
	return request(t, e.tc.Router, method, path, e.owner.token, body).Code
}

func (e *deadEnv) taskRoutes(id uint) []struct{ method, path string } {
	base := "/api/v1/tasks/" + itoa(id)
	return []struct{ method, path string }{
		{http.MethodGet, base},
		{http.MethodPatch, base},
		{http.MethodDelete, base},
		{http.MethodPost, base + "/move"},
		{http.MethodGet, base + "/comments"},
		{http.MethodPost, base + "/comments"},
		{http.MethodGet, base + "/attachments"},
		{http.MethodGet, base + "/checklists"},
		{http.MethodPost, base + "/checklists"},
		{http.MethodGet, base + "/watchers"},
		{http.MethodPut, base + "/watch"},
		{http.MethodGet, base + "/activity"},
		{http.MethodGet, base + "/custom-fields"},
	}
}

func (e *deadEnv) assertTaskGone(t *testing.T, id uint, label string) {
	t.Helper()
	for _, route := range e.taskRoutes(id) {
		code := e.do(t, route.method, route.path, map[string]any{"title": "x", "body": "x", "name": "x", "status_id": e.statusID})
		if code != http.StatusNotFound && code != http.StatusForbidden {
			t.Errorf("%s: %s %s returned %d, want 403 or 404", label, route.method, route.path, code)
		}
	}
}

func (e *deadEnv) assertListGone(t *testing.T, label string) {
	t.Helper()
	listPath := "/api/v1/lists/" + itoa(e.listID)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, listPath + "/tasks"},
		{http.MethodPost, listPath + "/tasks"},
		{http.MethodGet, listPath + "/statuses"},
		{http.MethodPost, listPath + "/events/ticket"},
	} {
		code := e.do(t, route.method, route.path, map[string]any{"title": "x", "status_id": e.statusID})
		if code != http.StatusNotFound && code != http.StatusForbidden {
			t.Errorf("%s: %s %s returned %d, want 403 or 404", label, route.method, route.path, code)
		}
	}
}

func (e *deadEnv) assertAlive(t *testing.T) {
	t.Helper()
	for _, id := range []uint{e.parentID, e.childID, e.taskID} {
		if code := e.do(t, http.MethodGet, "/api/v1/tasks/"+itoa(id), nil); code != http.StatusOK {
			t.Fatalf("task %d before delete: %d", id, code)
		}
	}
	if code := e.do(t, http.MethodGet, "/api/v1/lists/"+itoa(e.listID)+"/tasks", nil); code != http.StatusOK {
		t.Fatalf("list tasks before delete: %d", code)
	}
}

func (e *deadEnv) assertEverythingGone(t *testing.T, label string) {
	t.Helper()
	for _, id := range []uint{e.parentID, e.childID, e.taskID} {
		e.assertTaskGone(t, id, fmt.Sprintf("%s task %d", label, id))
	}
	e.assertListGone(t, label)
}

func TestDeadData_DeletingAListHidesItsTasks(t *testing.T) {
	e := newDeadEnv(t)
	e.assertAlive(t)
	if code := e.do(t, http.MethodDelete, "/api/v1/lists/"+itoa(e.listID), nil); code != http.StatusOK {
		t.Fatalf("delete list: %d", code)
	}
	e.assertEverythingGone(t, "deleted list")
}

func TestDeadData_DeletingAFolderHidesItsListsAndTasks(t *testing.T) {
	e := newDeadEnv(t)
	e.assertAlive(t)
	if code := e.do(t, http.MethodDelete, "/api/v1/folders/"+itoa(e.folderID), nil); code != http.StatusOK {
		t.Fatalf("delete folder: %d", code)
	}
	e.assertEverythingGone(t, "deleted folder")
	if code := e.do(t, http.MethodPost, "/api/v1/folders/"+itoa(e.folderID)+"/lists", map[string]any{"name": "new"}); code != http.StatusNotFound && code != http.StatusForbidden {
		t.Errorf("create list in a deleted folder: %d", code)
	}
}

func TestDeadData_DeletingASpaceHidesItsContent(t *testing.T) {
	e := newDeadEnv(t)
	e.assertAlive(t)
	if code := e.do(t, http.MethodDelete, "/api/v1/spaces/"+itoa(e.owner.spaceID), nil); code != http.StatusOK {
		t.Fatalf("delete space: %d", code)
	}
	e.assertEverythingGone(t, "deleted space")
	for _, path := range []string{"/dashboard", "/lists", "/folders", "/events/ticket"} {
		method := http.MethodGet
		if path == "/events/ticket" {
			method = http.MethodPost
		}
		code := e.do(t, method, "/api/v1/spaces/"+itoa(e.owner.spaceID)+path, nil)
		if code != http.StatusNotFound && code != http.StatusForbidden {
			t.Errorf("deleted space %s %s: %d", method, path, code)
		}
	}
}

func TestDeadData_DeletingAWorkspaceHidesItsContent(t *testing.T) {
	e := newDeadEnv(t)
	e.assertAlive(t)
	if code := e.do(t, http.MethodDelete, "/api/v1/workspaces/"+itoa(e.owner.workspaceID), nil); code != http.StatusOK {
		t.Fatalf("delete workspace: %d", code)
	}
	e.assertEverythingGone(t, "deleted workspace")
}

func TestDeadData_DeletingAParentTaskHidesItsSubtasks(t *testing.T) {
	e := newDeadEnv(t)
	e.assertAlive(t)
	if code := e.do(t, http.MethodDelete, "/api/v1/tasks/"+itoa(e.parentID), nil); code != http.StatusOK {
		t.Fatalf("delete parent: %d", code)
	}
	e.assertTaskGone(t, e.parentID, "deleted parent")
	e.assertTaskGone(t, e.childID, "subtask of a deleted parent")
	if code := e.do(t, http.MethodGet, "/api/v1/tasks/"+itoa(e.taskID), nil); code != http.StatusOK {
		t.Fatalf("unrelated task after parent delete: %d", code)
	}
	var live int64
	if err := e.tc.DB.Raw("SELECT count(*) FROM tasks WHERE id IN (?, ?) AND deleted_at IS NULL", e.parentID, e.childID).Scan(&live).Error; err != nil || live != 0 {
		t.Fatalf("subtask was not soft-deleted with its parent: live=%d err=%v", live, err)
	}
}

func TestDeadData_CascadeSharesOneTimestampAndSkipsEarlierDeletes(t *testing.T) {
	e := newDeadEnv(t)
	if code := e.do(t, http.MethodDelete, "/api/v1/tasks/"+itoa(e.taskID), nil); code != http.StatusOK {
		t.Fatalf("delete task: %d", code)
	}
	var before string
	if err := e.tc.DB.Raw("SELECT deleted_at::text FROM tasks WHERE id = ?", e.taskID).Scan(&before).Error; err != nil {
		t.Fatal(err)
	}
	if code := e.do(t, http.MethodDelete, "/api/v1/lists/"+itoa(e.listID), nil); code != http.StatusOK {
		t.Fatalf("delete list: %d", code)
	}
	var after string
	if err := e.tc.DB.Raw("SELECT deleted_at::text FROM tasks WHERE id = ?", e.taskID).Scan(&after).Error; err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("an already deleted task was touched by the cascade: %s -> %s", before, after)
	}
	var distinct int64
	if err := e.tc.DB.Raw("SELECT count(DISTINCT deleted_at) FROM tasks WHERE id IN (?, ?)", e.parentID, e.childID).Scan(&distinct).Error; err != nil || distinct != 1 {
		t.Fatalf("cascaded tasks must share one deleted_at: %d err=%v", distinct, err)
	}
}

func TestDeadData_AncestorsHiddenEvenWithoutCascade(t *testing.T) {
	cases := []struct {
		name string
		sql  func(e *deadEnv) (string, []any)
	}{
		{"list only", func(e *deadEnv) (string, []any) {
			return "UPDATE lists SET deleted_at = now() WHERE id = ?", []any{e.listID}
		}},
		{"folder only", func(e *deadEnv) (string, []any) {
			return "UPDATE folders SET deleted_at = now() WHERE id = ?", []any{e.folderID}
		}},
		{"space only", func(e *deadEnv) (string, []any) {
			return "UPDATE spaces SET deleted_at = now() WHERE id = ?", []any{e.owner.spaceID}
		}},
		{"workspace only", func(e *deadEnv) (string, []any) {
			return "UPDATE workspaces SET deleted_at = now() WHERE id = ?", []any{e.owner.workspaceID}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newDeadEnv(t)
			e.assertAlive(t)
			stmt, args := tc.sql(e)
			if err := e.tc.DB.Exec(stmt, args...).Error; err != nil {
				t.Fatal(err)
			}
			e.assertEverythingGone(t, tc.name)
		})
	}

	t.Run("parent task only", func(t *testing.T) {
		e := newDeadEnv(t)
		if err := e.tc.DB.Exec("UPDATE tasks SET deleted_at = now() WHERE id = ?", e.parentID).Error; err != nil {
			t.Fatal(err)
		}
		e.assertTaskGone(t, e.childID, "subtask")
	})
}

func TestDeadData_NotificationsOfHiddenTasksDisappear(t *testing.T) {
	tc := newTestContext(t)
	f := newNotifyFixture(t, tc)
	f.newTask(t, "assigned", []uint{f.memberA})
	tc.Notifications.Wait()
	if got := inbox(t, tc, f.tokenA, ""); len(got) == 0 {
		t.Fatal("expected a notification before the delete")
	}
	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/lists/"+itoa(f.listID), f.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete list: %d: %s", rec.Code, rec.Body.String())
	}
	if got := inbox(t, tc, f.tokenA, ""); len(got) != 0 {
		t.Fatalf("notifications of tasks in a deleted list are still visible: %+v", got)
	}
}

func TestDeadData_RealtimeTicketsAreRefusedForDeletedScopes(t *testing.T) {
	e := newDeadEnv(t)
	for _, path := range []string{
		"/api/v1/lists/" + itoa(e.listID) + "/events/ticket",
		"/api/v1/spaces/" + itoa(e.owner.spaceID) + "/events/ticket",
	} {
		if code := e.do(t, http.MethodPost, path, nil); code != http.StatusOK && code != http.StatusCreated {
			t.Fatalf("ticket before delete %s: %d", path, code)
		}
	}
	if code := e.do(t, http.MethodDelete, "/api/v1/folders/"+itoa(e.folderID), nil); code != http.StatusOK {
		t.Fatalf("delete folder: %d", code)
	}
	if code := e.do(t, http.MethodPost, "/api/v1/lists/"+itoa(e.listID)+"/events/ticket", nil); code != http.StatusNotFound && code != http.StatusForbidden {
		t.Fatalf("ticket for a list in a deleted folder: %d", code)
	}
}
