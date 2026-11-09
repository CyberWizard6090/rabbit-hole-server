package integration

import (
	"net/http"
	"strings"
	"sync"
	"testing"
)

type relEnv struct {
	tc       *TestContext
	owner    workspaceOwner
	guest    string
	outsider string
	list     uint
	todo     uint
	done     uint
	tasks    []uint
	foreign  uint
}

func newRelEnv(t *testing.T) *relEnv {
	t.Helper()
	tc := newTestContext(t)
	e := &relEnv{tc: tc, owner: newWorkspaceOwner(t, tc, "relations")}
	guestID, guest := newUser(t, tc, "rel-guest@example.test")
	_, e.outsider = newUser(t, tc, "rel-outsider@example.test")
	e.guest = guest
	addContact(t, tc, e.owner.token, guestID)
	if rec := request(t, tc.Router, http.MethodPost, membersPath(e.owner.workspaceID, ""), e.owner.token,
		map[string]any{"user_id": guestID, "role": "Guest"}); rec.Code != http.StatusCreated {
		t.Fatalf("add guest: %d: %s", rec.Code, rec.Body.String())
	}
	e.list = mustCreate(t, tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/lists", map[string]any{"name": "L"})
	e.todo = mustCreate(t, tc, e.owner.token, "/api/v1/lists/"+itoa(e.list)+"/statuses", map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": e.list})
	e.done = mustCreate(t, tc, e.owner.token, "/api/v1/lists/"+itoa(e.list)+"/statuses", map[string]any{"name": "Done", "color": "#112233", "type": 3, "list_id": e.list})
	for _, title := range []string{"A", "B", "C", "D"} {
		e.tasks = append(e.tasks, mustCreate(t, tc, e.owner.token, "/api/v1/lists/"+itoa(e.list)+"/tasks", map[string]any{"title": title, "status_id": e.todo}))
	}
	foreign := newWorkspaceOwner(t, tc, "relforeign")
	fl := mustCreate(t, tc, foreign.token, "/api/v1/spaces/"+itoa(foreign.spaceID)+"/lists", map[string]any{"name": "F"})
	fs := mustCreate(t, tc, foreign.token, "/api/v1/lists/"+itoa(fl)+"/statuses", map[string]any{"name": "F", "color": "#112233", "type": 1, "list_id": fl})
	e.foreign = mustCreate(t, tc, foreign.token, "/api/v1/lists/"+itoa(fl)+"/tasks", map[string]any{"title": "foreign", "status_id": fs})
	return e
}

func relPath(task uint, suffix string) string {
	return "/api/v1/tasks/" + itoa(task) + "/relations" + suffix
}

func (e *relEnv) add(t *testing.T, token string, task, other uint, kind string, want int) {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPost, relPath(task, ""), token, map[string]any{"task_id": other, "kind": kind})
	if rec.Code != want {
		t.Fatalf("add %d %s %d: %d (want %d): %s", task, kind, other, rec.Code, want, rec.Body.String())
	}
}

func (e *relEnv) get(t *testing.T, token string, task uint) map[string]any {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, relPath(task, ""), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("relations of %d: %d: %s", task, rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)
}

func relIDs(group any) []uint {
	var out []uint
	for _, item := range group.([]any) {
		out = append(out, uint(item.(map[string]any)["id"].(float64)))
	}
	return out
}

func TestTaskRelations_BlocksBlockedByAndRelatedAreSeenFromBothSides(t *testing.T) {
	e := newRelEnv(t)
	a, b, c, d := e.tasks[0], e.tasks[1], e.tasks[2], e.tasks[3]
	e.add(t, e.owner.token, a, b, "blocks", http.StatusCreated)
	e.add(t, e.owner.token, c, a, "blocked_by", http.StatusCreated)
	e.add(t, e.owner.token, d, a, "related", http.StatusCreated)

	ra := e.get(t, e.owner.token, a)
	if got := relIDs(ra["blocks"]); len(got) != 2 {
		t.Fatalf("A blocks %v, want B and C", got)
	}
	if got := relIDs(ra["related"]); len(got) != 1 || got[0] != d {
		t.Fatalf("A related = %v", got)
	}
	rb := e.get(t, e.owner.token, b)
	if got := relIDs(rb["blocked_by"]); len(got) != 1 || got[0] != a || rb["blocked"] != true {
		t.Fatalf("B = %v", rb)
	}
	if got := relIDs(e.get(t, e.owner.token, d)["related"]); len(got) != 1 || got[0] != a {
		t.Fatalf("related is symmetric, D sees %v", got)
	}

	if rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(a), e.owner.token, map[string]any{"status_id": e.done}); rec.Code != http.StatusOK {
		t.Fatalf("complete A: %d: %s", rec.Code, rec.Body.String())
	}
	rb = e.get(t, e.owner.token, b)
	blocker := rb["blocked_by"].([]any)[0].(map[string]any)
	if blocker["done"] != true || rb["blocked"] != false {
		t.Fatalf("once the blocker is done B is free: %v", rb)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM task_activities WHERE task_id = ? AND action = 'relation.added'", a); n != 1 {
		t.Fatalf("relation history rows on A = %d, want 1", n)
	}
}

func TestTaskRelations_CyclesDuplicatesAndLimits(t *testing.T) {
	e := newRelEnv(t)
	a, b, c := e.tasks[0], e.tasks[1], e.tasks[2]
	e.add(t, e.owner.token, a, b, "blocks", http.StatusCreated)
	e.add(t, e.owner.token, b, c, "blocks", http.StatusCreated)
	for name, args := range map[string][3]any{
		"C blocks A (a circle)":     {c, a, "blocks"},
		"B blocks A (back)":         {b, a, "blocks"},
		"A blocked by C (a circle)": {a, c, "blocked_by"},
	} {
		rec := request(t, e.tc.Router, http.MethodPost, relPath(args[0].(uint), ""), e.owner.token, map[string]any{"task_id": args[1], "kind": args[2]})
		if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "RELATION_CYCLE") {
			t.Errorf("%s: %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	e.add(t, e.owner.token, a, b, "blocks", http.StatusConflict)
	e.add(t, e.owner.token, a, a, "related", http.StatusBadRequest)
	e.add(t, e.owner.token, a, e.foreign, "blocks", http.StatusNotFound)
	e.add(t, e.owner.token, a, 99999999, "related", http.StatusNotFound)
	e.add(t, e.owner.token, a, c, "friends", http.StatusBadRequest)
	e.add(t, e.owner.token, a, c, "related", http.StatusCreated)
	e.add(t, e.owner.token, c, a, "related", http.StatusConflict)
}

func TestTaskRelations_OppositeBlocksAddedAtTheSameTimeNeverFormACircle(t *testing.T) {
	e := newRelEnv(t)
	a, b := e.tasks[0], e.tasks[1]
	var group sync.WaitGroup
	codes := make(chan int, 2)
	for _, pair := range [][2]uint{{a, b}, {b, a}} {
		group.Add(1)
		go func() {
			defer group.Done()
			rec := request(t, e.tc.Router, http.MethodPost, relPath(pair[0], ""), e.owner.token, map[string]any{"task_id": pair[1], "kind": "blocks"})
			codes <- rec.Code
		}()
	}
	group.Wait()
	close(codes)
	created, refused := 0, 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			refused++
		}
	}
	if created != 1 || refused != 1 {
		t.Fatalf("created=%d refused=%d, want exactly one of each", created, refused)
	}
}

func TestTaskRelations_LimitPerTask(t *testing.T) {
	e := newRelEnv(t)
	a := e.tasks[0]
	for i := 0; i < 50; i++ {
		other := mustCreate(t, e.tc, e.owner.token, "/api/v1/lists/"+itoa(e.list)+"/tasks", map[string]any{"title": "x" + itoa(uint(i)), "status_id": e.todo})
		e.add(t, e.owner.token, a, other, "related", http.StatusCreated)
	}
	e.add(t, e.owner.token, a, e.tasks[1], "related", http.StatusConflict)
}

func TestTaskRelations_PermissionsRemovalAndDeletedTasks(t *testing.T) {
	e := newRelEnv(t)
	a, b := e.tasks[0], e.tasks[1]
	e.add(t, e.guest, a, b, "blocks", http.StatusForbidden)
	e.add(t, e.outsider, a, b, "blocks", http.StatusForbidden)
	e.add(t, e.owner.token, a, b, "blocks", http.StatusCreated)
	if got := relIDs(e.get(t, e.guest, a)["blocks"]); len(got) != 1 {
		t.Fatalf("a guest reads the relations: %v", got)
	}
	if rec := request(t, e.tc.Router, http.MethodGet, relPath(a, ""), e.outsider, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider read: %d", rec.Code)
	}
	for _, token := range []string{e.guest, e.outsider} {
		if rec := request(t, e.tc.Router, http.MethodDelete, relPath(a, "/"+itoa(b)+"?kind=blocks"), token, nil); rec.Code != http.StatusForbidden {
			t.Fatalf("remove without permission: %d", rec.Code)
		}
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, relPath(a, "/"+itoa(b)), e.owner.token, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("remove without a kind: %d", rec.Code)
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, relPath(b, "/"+itoa(a)+"?kind=blocked_by"), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove from the other side: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, e.tc.Router, http.MethodDelete, relPath(b, "/"+itoa(a)+"?kind=blocked_by"), e.owner.token, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("remove twice: %d", rec.Code)
	}
	e.add(t, e.owner.token, a, b, "blocks", http.StatusCreated)
	if rec := request(t, e.tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(b), e.owner.token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete B: %d", rec.Code)
	}
	if got := relIDs(e.get(t, e.owner.token, a)["blocks"]); len(got) != 0 {
		t.Fatalf("a deleted task is still listed as blocked: %v", got)
	}
	e.add(t, e.owner.token, a, b, "related", http.StatusNotFound)
}
