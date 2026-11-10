package integration

import (
	"net/http"
	"net/url"
	"testing"
)

type slEnv struct {
	env      fcEnv
	tc       *TestContext
	parentA  uint
	parentB  uint
	childA   []uint
	childB   uint
	topLevel uint
}

func slSetup(t *testing.T) slEnv {
	t.Helper()
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "sltasks")
	e := slEnv{env: env, tc: tc}
	mk := func(title string, extra map[string]any) uint {
		body := map[string]any{"title": title, "status_id": env.statusID}
		for k, v := range extra {
			body[k] = v
		}
		return mustCreate(t, tc, env.owner.token, rfTasksURL(env), body)
	}
	e.parentA = mk("parent A", nil)
	e.parentB = mk("parent B", nil)
	e.topLevel = mk("lonely", nil)
	e.childA = []uint{mk("A1", map[string]any{"parent_id": e.parentA}), mk("A2", map[string]any{"parent_id": e.parentA}), mk("A3", map[string]any{"parent_id": e.parentA})}
	e.childB = mk("B1", map[string]any{"parent_id": e.parentB})
	return e
}

func (e slEnv) list(t *testing.T, query string) (ids []uint, body map[string]any) {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, rfTasksURL(e.env)+query, e.env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list %q: %d: %s", query, rec.Code, rec.Body.String())
	}
	body = dataObject(t, rec)
	for _, item := range body["items"].([]any) {
		ids = append(ids, uint(item.(map[string]any)["id"].(float64)))
	}
	return ids, body
}

func slSameSet(got, want []uint) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[uint]bool{}
	for _, id := range got {
		seen[id] = true
	}
	for _, id := range want {
		if !seen[id] {
			return false
		}
	}
	return true
}

func TestTaskList_SubtaskCountAndParentFilters(t *testing.T) {
	e := slSetup(t)

	_, body := e.list(t, "")
	counts := map[float64]float64{}
	for _, item := range body["items"].([]any) {
		task := item.(map[string]any)
		count, present := task["subtask_count"]
		if !present {
			t.Fatalf("task %v has no subtask_count", task["id"])
		}
		counts[task["id"].(float64)] = count.(float64)
	}
	for id, want := range map[uint]float64{e.parentA: 3, e.parentB: 1, e.topLevel: 0, e.childA[0]: 0} {
		if counts[float64(id)] != want {
			t.Errorf("subtask_count of task %d = %v, want %v", id, counts[float64(id)], want)
		}
	}

	if ids, _ := e.list(t, "?parent_id="+itoa(e.parentA)); !slSameSet(ids, e.childA) {
		t.Errorf("parent_id=A returned %v, want %v", ids, e.childA)
	}
	if ids, _ := e.list(t, "?has_parent=false"); !slSameSet(ids, []uint{e.parentA, e.parentB, e.topLevel}) {
		t.Errorf("has_parent=false returned %v", ids)
	}
	if ids, _ := e.list(t, "?has_parent=true"); !slSameSet(ids, append(append([]uint{}, e.childA...), e.childB)) {
		t.Errorf("has_parent=true returned %v", ids)
	}
	if ids, _ := e.list(t, "?parent_id="+itoa(e.parentA)+","+itoa(e.parentB)); len(ids) != 4 {
		t.Errorf("parent_id=A,B returned %v, want 4 children", ids)
	}
	if ids, _ := e.list(t, "?parent_id="+itoa(e.topLevel)); len(ids) != 0 {
		t.Errorf("parent without children returned %v", ids)
	}
	_, withTotal := e.list(t, "?parent_id="+itoa(e.parentA)+"&with_total=true")
	if withTotal["total_count"] != float64(3) {
		t.Errorf("total_count = %v, want 3", withTotal["total_count"])
	}

	rec := request(t, e.tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(e.childA[0]), e.env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete child: %d", rec.Code)
	}
	items, _ := e.list(t, "?parent_id="+itoa(e.parentA))
	if len(items) != 2 {
		t.Errorf("deleted child still listed: %v", items)
	}
	rec = request(t, e.tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(e.parentA), e.env.owner.token, nil)
	if got := dataObject(t, rec)["subtask_count"]; got != float64(2) {
		t.Errorf("subtask_count of the single task = %v, want 2 live children", got)
	}
}

func TestTaskList_ParentFilterValidationAndConflicts(t *testing.T) {
	e := slSetup(t)
	for _, query := range []string{
		"?parent_id=abc", "?parent_id=0", "?parent_id=-1", "?has_parent=maybe",
		"?parent_id=" + itoa(e.parentA) + "&has_parent=false",
	} {
		rec := request(t, e.tc.Router, http.MethodGet, rfTasksURL(e.env)+query, e.env.owner.token, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", query, rec.Code, rec.Body.String())
			continue
		}
		if code := fcErrorBody(t, rec)["code"]; code != "INVALID_FILTER" {
			t.Errorf("%s: code = %v, want INVALID_FILTER", query, code)
		}
	}
	if ids, _ := e.list(t, "?parent_id="+itoa(e.parentA)+"&has_parent=true"); len(ids) != 3 {
		t.Errorf("parent_id with has_parent=true should still work, got %v", ids)
	}
}

func TestTaskList_ParentFilterKeepsKeysetPagination(t *testing.T) {
	e := slSetup(t)
	base := "?parent_id=" + itoa(e.parentA) + "&limit=2&sort=title"
	first, body := e.list(t, base)
	if len(first) != 2 || body["has_next"] != true {
		t.Fatalf("first page = %v, has_next=%v", first, body["has_next"])
	}
	cursor := body["next_cursor"].(string)
	second, body := e.list(t, base+"&cursor="+url.QueryEscape(cursor))
	if len(second) != 1 || body["has_next"] != false {
		t.Fatalf("second page = %v, has_next=%v", second, body["has_next"])
	}
	if !slSameSet(append(first, second...), e.childA) {
		t.Fatalf("pages returned %v + %v, want %v", first, second, e.childA)
	}

	rec := request(t, e.tc.Router, http.MethodGet,
		rfTasksURL(e.env)+"?parent_id="+itoa(e.parentB)+"&limit=2&sort=title&cursor="+url.QueryEscape(cursor), e.env.owner.token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("cursor from another filter: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	topFirst, topBody := e.list(t, "?has_parent=false&limit=2&sort=title")
	if len(topFirst) != 2 || topBody["has_next"] != true {
		t.Fatalf("top-level first page = %v", topFirst)
	}
	topSecond, _ := e.list(t, "?has_parent=false&limit=2&sort=title&cursor="+url.QueryEscape(topBody["next_cursor"].(string)))
	if !slSameSet(append(topFirst, topSecond...), []uint{e.parentA, e.parentB, e.topLevel}) {
		t.Fatalf("top-level pages = %v + %v", topFirst, topSecond)
	}
}

func TestTaskList_SubtaskCountOnOtherTaskEndpoints(t *testing.T) {
	e := slSetup(t)
	rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(e.parentA), e.env.owner.token, map[string]any{"title": "parent A renamed"})
	if rec.Code != http.StatusOK || dataObject(t, rec)["subtask_count"] != float64(3) {
		t.Fatalf("update response: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, e.tc.Router, http.MethodPost, rfTasksURL(e.env), e.env.owner.token,
		map[string]any{"title": "fresh", "status_id": e.env.statusID})
	if rec.Code != http.StatusCreated || dataObject(t, rec)["subtask_count"] != float64(0) {
		t.Fatalf("create response: %d: %s", rec.Code, rec.Body.String())
	}
}
