package integration

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
)

type cncEnv struct {
	tc      *TestContext
	owner   workspaceOwner
	listID  uint
	statusA uint
	statusB uint
	taskID  uint
}

func newCncEnv(t *testing.T) *cncEnv {
	t.Helper()
	tc := newTestContext(t)
	owner := newWorkspaceOwner(t, tc, "cnc")
	listID := mustCreate(t, tc, owner.token, "/api/v1/spaces/"+itoa(owner.spaceID)+"/lists", map[string]any{"name": "list"})
	e := &cncEnv{tc: tc, owner: owner, listID: listID}
	e.statusA = e.status(t, listID, "A")
	e.statusB = e.status(t, listID, "B")
	e.taskID = e.task(t, listID, e.statusA, "original title")
	return e
}

func (e *cncEnv) status(t *testing.T, listID uint, name string) uint {
	t.Helper()
	return mustCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(listID)+"/statuses",
		map[string]any{"name": name, "color": "#112233", "type": 1, "list_id": listID})
}

func (e *cncEnv) task(t *testing.T, listID, statusID uint, title string) uint {
	t.Helper()
	return mustCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(listID)+"/tasks",
		map[string]any{"title": title, "status_id": statusID})
}

func (e *cncEnv) patch(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	return request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(e.taskID), e.owner.token, body)
}

type taskRow struct {
	ID          uint
	ListID      uint
	SpaceID     uint
	StatusID    uint
	Title       string
	Description string
	Priority    int
	Position    int64
	DeletedAt   *time.Time
}

func (e *cncEnv) row(t *testing.T) taskRow {
	t.Helper()
	var row taskRow
	if err := e.tc.DB.Raw(
		"SELECT id, list_id, space_id, status_id, title, coalesce(description, '') AS description, priority, position, deleted_at FROM tasks WHERE id = ?",
		e.taskID,
	).Scan(&row).Error; err != nil {
		t.Fatalf("read task: %v", err)
	}
	return row
}

func waitForLockWait(t *testing.T, db *gorm.DB) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var n int64
		if err := db.Raw(
			"SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock' AND (query ILIKE '%for update%' OR query ILIKE '%for no key update%')",
		).Scan(&n).Error; err != nil {
			t.Fatalf("poll locks: %v", err)
		}
		if n > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the request never blocked on the row lock")
}

func (e *cncEnv) racePatch(t *testing.T, body map[string]any, concurrent func(tx *gorm.DB)) *httptest.ResponseRecorder {
	t.Helper()
	tx := e.tc.DB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	if err := tx.Exec("SELECT id FROM tasks WHERE id = ? FOR UPDATE", e.taskID).Error; err != nil {
		tx.Rollback()
		t.Fatalf("lock row: %v", err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- e.patch(t, body) }()
	waitForLockWait(t, e.tc.DB)
	concurrent(tx)
	if err := tx.Commit().Error; err != nil {
		t.Fatalf("commit: %v", err)
	}
	select {
	case rec := <-done:
		return rec
	case <-time.After(15 * time.Second):
		t.Fatal("patch did not finish after the lock was released")
		return nil
	}
}

func TestTaskConcurrency_PatchDoesNotRevertAConcurrentStatusAndPositionChange(t *testing.T) {
	e := newCncEnv(t)
	rec := e.racePatch(t, map[string]any{"title": "patched title"}, func(tx *gorm.DB) {
		if err := tx.Exec("UPDATE tasks SET status_id = ?, position = 777000 WHERE id = ?", e.statusB, e.taskID).Error; err != nil {
			t.Fatalf("simulate move: %v", err)
		}
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d: %s", rec.Code, rec.Body.String())
	}
	row := e.row(t)
	if row.Title != "patched title" || row.StatusID != e.statusB || row.Position != 777000 {
		t.Fatalf("lost update: %+v", row)
	}
}

func TestTaskConcurrency_PatchDoesNotRevertAConcurrentMoveToAnotherList(t *testing.T) {
	e := newCncEnv(t)
	otherList := mustCreate(t, e.tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/lists", map[string]any{"name": "other"})
	otherStatus := e.status(t, otherList, "Other")
	rec := e.racePatch(t, map[string]any{"title": "patched title", "priority": 4}, func(tx *gorm.DB) {
		if err := tx.Exec("UPDATE tasks SET list_id = ?, status_id = ?, position = 4242 WHERE id = ?", otherList, otherStatus, e.taskID).Error; err != nil {
			t.Fatalf("simulate move: %v", err)
		}
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d: %s", rec.Code, rec.Body.String())
	}
	row := e.row(t)
	if row.Title != "patched title" || row.Priority != 4 || row.ListID != otherList || row.StatusID != otherStatus || row.Position != 4242 {
		t.Fatalf("lost update: %+v", row)
	}
}

func TestTaskConcurrency_PatchDoesNotResurrectAConcurrentlyDeletedTask(t *testing.T) {
	e := newCncEnv(t)
	rec := e.racePatch(t, map[string]any{"title": "patched title"}, func(tx *gorm.DB) {
		if err := tx.Exec("UPDATE tasks SET deleted_at = now() WHERE id = ?", e.taskID).Error; err != nil {
			t.Fatalf("simulate delete: %v", err)
		}
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("patch of a deleted task: expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
	row := e.row(t)
	if row.DeletedAt == nil || row.Title != "original title" {
		t.Fatalf("deleted task was modified or restored: %+v", row)
	}
}

func TestTaskConcurrency_PatchKeepsAConcurrentlyChangedOtherField(t *testing.T) {
	e := newCncEnv(t)
	rec := e.racePatch(t, map[string]any{"title": "patched title"}, func(tx *gorm.DB) {
		if err := tx.Exec("UPDATE tasks SET description = 'written meanwhile', priority = 5 WHERE id = ?", e.taskID).Error; err != nil {
			t.Fatalf("simulate patch: %v", err)
		}
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("patch: %d: %s", rec.Code, rec.Body.String())
	}
	row := e.row(t)
	if row.Title != "patched title" || row.Description != "written meanwhile" || row.Priority != 5 {
		t.Fatalf("lost update: %+v", row)
	}
}

func TestTaskConcurrency_ParallelPatchesOfDifferentFieldsAllSurvive(t *testing.T) {
	e := newCncEnv(t)
	const rounds = 15
	writers := []func(i int) map[string]any{
		func(i int) map[string]any { return map[string]any{"title": "title " + itoa(uint(i)+100)} },
		func(i int) map[string]any { return map[string]any{"description": "description " + itoa(uint(i))} },
		func(i int) map[string]any { return map[string]any{"priority": i%5 + 1} },
		func(i int) map[string]any { return map[string]any{"time_estimate": i + 1} },
	}
	var wg sync.WaitGroup
	failures := make(chan string, len(writers)*rounds)
	for _, build := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < rounds; i++ {
				if rec := e.patch(t, build(i)); rec.Code != http.StatusOK {
					failures <- rec.Body.String()
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for f := range failures {
		t.Fatalf("patch failed: %s", f)
	}
	last := rounds - 1
	var estimate int
	if err := e.tc.DB.Raw("SELECT time_estimate FROM tasks WHERE id = ?", e.taskID).Scan(&estimate).Error; err != nil {
		t.Fatal(err)
	}
	row := e.row(t)
	if row.Title != "title "+itoa(uint(last)+100) || row.Description != "description "+itoa(uint(last)) ||
		row.Priority != last%5+1 || estimate != last+1 {
		t.Fatalf("a concurrent patch was lost: %+v estimate=%d", row, estimate)
	}
}

func TestTaskConcurrency_ParallelMovesAndPatchesKeepTheMove(t *testing.T) {
	e := newCncEnv(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			e.patch(t, map[string]any{"description": "d" + itoa(uint(i))})
		}()
		go func() {
			defer wg.Done()
			target := e.statusB
			if i%2 == 1 {
				target = e.statusA
			}
			request(t, e.tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(e.taskID)+"/move", e.owner.token,
				map[string]any{"status_id": target})
		}()
	}
	wg.Wait()
	final := request(t, e.tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(e.taskID)+"/move", e.owner.token,
		map[string]any{"status_id": e.statusB})
	if final.Code != http.StatusOK {
		t.Fatalf("final move: %d: %s", final.Code, final.Body.String())
	}
	e.patch(t, map[string]any{"title": "after"})
	if row := e.row(t); row.StatusID != e.statusB || row.Title != "after" {
		t.Fatalf("unexpected final state: %+v", row)
	}
}

func TestTaskConcurrency_CreateRejectsAStatusRemovedWhileItWaited(t *testing.T) {
	e := newCncEnv(t)
	replacement := e.status(t, e.listID, "Replacement")
	tx := e.tc.DB.Begin()
	if err := tx.Exec("SELECT id FROM lists WHERE id = ? FOR UPDATE", e.listID).Error; err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(t, e.tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(e.listID)+"/tasks", e.owner.token,
			map[string]any{"title": "late task", "status_id": e.statusB})
	}()
	waitForLockWait(t, e.tc.DB)
	if err := tx.Exec("UPDATE task_statuses SET deleted_at = now() WHERE id = ?", e.statusB).Error; err != nil {
		tx.Rollback()
		t.Fatal(err)
	}
	if err := tx.Commit().Error; err != nil {
		t.Fatal(err)
	}
	rec := <-done
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("create with a removed status: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var n int64
	if err := e.tc.DB.Raw("SELECT count(*) FROM tasks WHERE status_id = ? AND deleted_at IS NULL", e.statusB).Scan(&n).Error; err != nil || n != 0 {
		t.Fatalf("a task references the removed status: n=%d err=%v", n, err)
	}
	if rec := e.task(t, e.listID, replacement, "ok"); rec == 0 {
		t.Fatal("create with a live status failed")
	}
}

func TestTaskSchema_RejectsStatusAndListMismatches(t *testing.T) {
	e := newCncEnv(t)
	otherSpace := mustCreate(t, e.tc, e.owner.token, "/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/spaces", map[string]any{"name": "other"})
	otherList := mustCreate(t, e.tc, e.owner.token, "/api/v1/spaces/"+itoa(otherSpace)+"/lists", map[string]any{"name": "other"})
	otherStatus := e.status(t, otherList, "Other")

	insert := "INSERT INTO tasks (space_id, list_id, status_id, title, user_id) VALUES (?, ?, ?, 'schema check', ?)"
	if err := e.tc.DB.Exec(insert, e.owner.spaceID, e.listID, otherStatus, e.owner.userID).Error; err == nil {
		t.Fatal("a task with a status of another list was accepted")
	}
	if err := e.tc.DB.Exec(insert, e.owner.spaceID, otherList, otherStatus, e.owner.userID).Error; err == nil {
		t.Fatal("a task whose list belongs to another space was accepted")
	}
	if err := e.tc.DB.Exec(insert, e.owner.spaceID, e.listID, e.statusA, e.owner.userID).Error; err != nil {
		t.Fatalf("a consistent task was rejected: %v", err)
	}
	if err := e.tc.DB.Exec("UPDATE tasks SET status_id = ? WHERE id = ?", otherStatus, e.taskID).Error; err == nil {
		t.Fatal("updating to a status of another list was accepted")
	}
	if err := e.tc.DB.Exec(
		"INSERT INTO task_statuses (space_id, list_id, name, color, position, type) SELECT ?, ?, 'bad', '#112233', 9, type FROM task_statuses WHERE id = ?",
		e.owner.spaceID, otherList, e.statusA,
	).Error; err == nil {
		t.Fatal("a status whose space differs from its list space was accepted")
	}
}

func TestTaskConcurrency_EditsOfDifferentTasksInOneListDoNotWaitForEachOther(t *testing.T) {
	e := newCncEnv(t)
	other := e.task(t, e.listID, e.statusA, "another task in the same list")

	tx := e.tc.DB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("SELECT id FROM lists WHERE id = ? FOR KEY SHARE", e.listID).Error; err != nil {
		t.Fatalf("share the list as an edit in flight does: %v", err)
	}

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(other), e.owner.token, map[string]any{"priority": 4})
	}()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("patch: %d: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("an edit waited for another edit of the same list: the list lock is not shared")
	}
}

func TestTaskConcurrency_EditWaitsForAnExclusiveListChange(t *testing.T) {
	e := newCncEnv(t)

	tx := e.tc.DB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	if err := tx.Exec("SELECT id FROM lists WHERE id = ? FOR UPDATE", e.listID).Error; err != nil {
		tx.Rollback()
		t.Fatalf("lock the list as a move, a delete or a create does: %v", err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- e.patch(t, map[string]any{"priority": 5}) }()
	select {
	case <-done:
		tx.Rollback()
		t.Fatal("an edit went through while the list was locked for a structural change")
	case <-time.After(400 * time.Millisecond):
	}
	tx.Rollback()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("patch after the release: %d: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the edit never finished after the list was released")
	}
	if e.row(t).Priority != 5 {
		t.Fatalf("priority = %d, want 5", e.row(t).Priority)
	}
}

func TestTaskConcurrency_EditsDoNotWaitForTasksBeingAppended(t *testing.T) {
	e := newCncEnv(t)

	tx := e.tc.DB.Begin()
	if tx.Error != nil {
		t.Fatalf("begin: %v", tx.Error)
	}
	defer tx.Rollback()
	if err := tx.Exec("SELECT id FROM lists WHERE id = ? FOR NO KEY UPDATE", e.listID).Error; err != nil {
		t.Fatalf("lock the list as a task creation does: %v", err)
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- e.patch(t, map[string]any{"priority": 4}) }()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("patch: %d: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("an edit waited for a task creation in the same list")
	}
}

func TestTaskConcurrency_CreationsOfOneListStayOrdered(t *testing.T) {
	e := newCncEnv(t)
	const creators = 12
	var group sync.WaitGroup
	ids := make(chan uint, creators)
	for i := 0; i < creators; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(e.listID)+"/tasks", e.owner.token,
				map[string]any{"title": "parallel", "status_id": e.statusA})
			if rec.Code != http.StatusCreated {
				t.Errorf("create: %d: %s", rec.Code, rec.Body.String())
				return
			}
			ids <- responseID(t, rec)
		}()
	}
	group.Wait()
	close(ids)
	var duplicates int64
	if err := e.tc.DB.Raw("SELECT count(*) - count(DISTINCT position) FROM tasks WHERE list_id = ?", e.listID).Scan(&duplicates).Error; err != nil {
		t.Fatal(err)
	}
	if duplicates != 0 {
		t.Fatalf("%d tasks share a position: parallel creations were not serialized", duplicates)
	}
}
