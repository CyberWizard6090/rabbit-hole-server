package integration

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

type timerEnv struct {
	tc     *TestContext
	owner  workspaceOwner
	listID uint
	status uint
}

func newTimerEnv(t *testing.T) *timerEnv {
	t.Helper()
	tc := newTestContext(t)
	e := &timerEnv{tc: tc, owner: newWorkspaceOwner(t, tc, "timerfix")}
	e.listID = mustCreate(t, tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/lists", map[string]any{"name": "L"})
	e.status = mustCreate(t, tc, e.owner.token, "/api/v1/lists/"+itoa(e.listID)+"/statuses",
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": e.listID})
	return e
}

func (e *timerEnv) task(t *testing.T, title string) uint {
	t.Helper()
	return mustCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(e.listID)+"/tasks", map[string]any{"title": title, "status_id": e.status})
}

func (e *timerEnv) start(t *testing.T, token string, taskID uint) int {
	t.Helper()
	return request(t, e.tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(taskID)+"/time/start", token, map[string]any{}).Code
}

func TestTime_ATimerOnADeletedTaskDoesNotBlockNewTimers(t *testing.T) {
	e := newTimerEnv(t)
	doomed, next := e.task(t, "doomed"), e.task(t, "next")
	if code := e.start(t, e.owner.token, doomed); code != http.StatusCreated {
		t.Fatalf("start: %d", code)
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(doomed), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete task: %d", rec.Code)
	}
	if code := e.start(t, e.owner.token, next); code != http.StatusCreated {
		t.Fatalf("a timer on a deleted task still blocks new timers: %d", code)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM task_time_entries WHERE user_id = ? AND ended_at IS NULL", e.owner.userID); n != 1 {
		t.Fatalf("running timers = %d, want exactly the new one", n)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM task_time_entries WHERE task_id = ? AND ended_at IS NOT NULL", doomed); n != 1 {
		t.Fatalf("the stranded timer was not closed (closed entries = %d)", n)
	}
}

func TestTime_ATimerLeftBehindInAWorkspaceDoesNotBlockNewTimers(t *testing.T) {
	e := newTimerEnv(t)
	memberID, memberToken := newUser(t, e.tc, fmt.Sprintf("timer-member-%d@example.test", time.Now().UnixNano()))
	addMemberWithRole(t, e.tc, e.owner.workspaceID, memberID, "Member")
	if code := e.start(t, memberToken, e.task(t, "theirs")); code != http.StatusCreated {
		t.Fatalf("member start: %d", code)
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, membersPath(e.owner.workspaceID, "/"+itoa(memberID)), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove member: %d: %s", rec.Code, rec.Body.String())
	}

	own := ownTaskFor(t, e.tc, memberToken)
	if code := e.start(t, memberToken, own); code != http.StatusCreated {
		t.Fatalf("a timer in a workspace the user left still blocks new timers: %d", code)
	}
}

// ownTaskFor gives an existing user a workspace of their own with one task and returns the task.
func ownTaskFor(t *testing.T, tc *TestContext, token string) uint {
	t.Helper()
	ws := mustCreate(t, tc, token, "/api/v1/workspaces", map[string]any{"name": "own"})
	sp := mustCreate(t, tc, token, "/api/v1/workspaces/"+itoa(ws)+"/spaces", map[string]any{"name": "own"})
	li := mustCreate(t, tc, token, "/api/v1/spaces/"+itoa(sp)+"/lists", map[string]any{"name": "own"})
	st := mustCreate(t, tc, token, "/api/v1/lists/"+itoa(li)+"/statuses", map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": li})
	return mustCreate(t, tc, token, "/api/v1/lists/"+itoa(li)+"/tasks", map[string]any{"title": "own", "status_id": st})
}

func TestTime_ARunningTimerOnAReachableTaskStillBlocksASecondOne(t *testing.T) {
	e := newTimerEnv(t)
	first, second := e.task(t, "first"), e.task(t, "second")
	if code := e.start(t, e.owner.token, first); code != http.StatusCreated {
		t.Fatalf("start: %d", code)
	}
	if code := e.start(t, e.owner.token, second); code != http.StatusConflict {
		t.Fatalf("second timer: %d, want 409 (one running timer per user)", code)
	}
}

func TestAccountDeletion_ForgetsTheSignInDevices(t *testing.T) {
	tc := newTestContext(t)
	email := fmt.Sprintf("forget-%d@example.test", time.Now().UnixNano())
	id, _ := newUser(t, tc, email)
	session := signInFrom(t, tc, email, chromeOnWindows, "203.0.113.7")
	if n := countRows(t, tc, "SELECT count(*) FROM login_devices WHERE user_id = ?", id); n == 0 {
		t.Fatal("sign-ins did not record a device; the test would prove nothing")
	}
	rec := request(t, tc.Router, http.MethodDelete, "/api/v1/profile", session.token, map[string]any{"password": "password-123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("delete account: %d: %s", rec.Code, rec.Body.String())
	}
	if n := countRows(t, tc, "SELECT count(*) FROM login_devices WHERE user_id = ?", id); n != 0 {
		t.Fatalf("%d sign-in devices outlived the account", n)
	}
}
