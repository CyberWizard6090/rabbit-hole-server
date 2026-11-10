package integration

import (
	"net/http"
	"reflect"
	"testing"
)

type activityItem struct {
	Action string
	Field  string
	Old    any
	New    any
	Actor  uint
}

func fetchActivity(t *testing.T, tc *TestContext, token string, taskID uint, query string) ([]activityItem, float64) {
	t.Helper()
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/activity"+query, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("activity: %d: %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	raw, _ := body["data"].([]any)
	items := make([]activityItem, 0, len(raw))
	for _, r := range raw {
		m := r.(map[string]any)
		item := activityItem{Action: m["action"].(string), Old: m["old_value"], New: m["new_value"]}
		if f, ok := m["field"].(string); ok {
			item.Field = f
		}
		item.Actor = uint(m["actor"].(map[string]any)["id"].(float64))
		items = append(items, item)
	}
	total := body["pagination"].(map[string]any)["total"].(float64)
	return items, total
}

func findActivity(items []activityItem, action, field string) *activityItem {
	for i := range items {
		if items[i].Action == action && items[i].Field == field {
			return &items[i]
		}
	}
	return nil
}

func TestActivity_RecordsTaskChangesCommentsAndMoves(t *testing.T) {
	env := newMoveEnv(t)
	tc := env.tc
	otherList := createList(t, tc, env.spaceID)
	otherStatus := env.status(t, otherList, "Other")

	taskID := env.task(t, env.listID, env.statusA, "tracked task")

	patch := func(body map[string]any) {
		t.Helper()
		if rec := request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), tc.Token, body); rec.Code != http.StatusOK {
			t.Fatalf("patch %v: %d: %s", body, rec.Code, rec.Body.String())
		}
	}
	patch(map[string]any{"title": "renamed task", "priority": 4})
	patch(map[string]any{"description": "a very private description", "assignee_ids": []uint{tc.UserID}})
	patch(map[string]any{"title": "renamed task"})
	patch(map[string]any{"due_date": "2026-05-10T00:00:00Z"})
	patch(map[string]any{"add_tag_names": []string{"urgent"}})

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(taskID)+"/comments", tc.Token, map[string]any{"body": "note"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("comment: %d: %s", rec.Code, rec.Body.String())
	}
	commentID := responseID(t, rec)
	request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID)+"/comments/"+itoa(commentID), tc.Token, map[string]any{"body": "edited note"})
	request(t, tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(taskID)+"/comments/"+itoa(commentID), tc.Token, nil)

	env.mustMove(t, taskID, map[string]any{"status_id": env.statusB})
	env.mustMove(t, taskID, map[string]any{"list_id": otherList, "status_id": otherStatus})
	env.mustMove(t, taskID, map[string]any{})

	items, total := fetchActivity(t, tc, tc.Token, taskID, "?limit=100")
	if int(total) != len(items) {
		t.Fatalf("total = %v but %d items", total, len(items))
	}
	if items[0].Action != "task.moved" || items[0].Field != "list_id" {
		t.Fatalf("newest entry = %+v, want the list move (the later reorder changes nothing)", items[0])
	}
	if last := items[len(items)-1]; last.Action != "task.created" {
		t.Fatalf("oldest entry = %+v, want task.created", last)
	}
	for _, it := range items {
		if it.Actor != tc.UserID {
			t.Fatalf("entry %+v has actor %d, want %d", it, it.Actor, tc.UserID)
		}
	}

	if e := findActivity(items, "task.updated", "title"); e == nil || e.Old != "tracked task" || e.New != "renamed task" {
		t.Fatalf("title entry = %+v", e)
	}
	titleEntries := 0
	for _, it := range items {
		if it.Field == "title" {
			titleEntries++
		}
	}
	if titleEntries != 1 {
		t.Fatalf("title entries = %d, repeating the same title must not log again", titleEntries)
	}
	if e := findActivity(items, "task.updated", "priority"); e == nil || e.Old != float64(2) || e.New != float64(4) {
		t.Fatalf("priority entry = %+v", e)
	}
	if e := findActivity(items, "task.updated", "description"); e == nil || e.Old != nil || e.New != nil {
		t.Fatalf("description entry must not carry text: %+v", e)
	}
	if e := findActivity(items, "task.updated", "assignee_ids"); e == nil || len(e.New.([]any)) != 1 || len(e.Old.([]any)) != 0 {
		t.Fatalf("assignee entry = %+v", e)
	}
	if e := findActivity(items, "task.updated", "due_date"); e == nil || e.Old != nil && e.Old != "null" {
		t.Fatalf("due_date entry = %+v", e)
	}
	if findActivity(items, "task.updated", "tag_ids") == nil {
		t.Fatal("tag change was not recorded")
	}
	var sawStatusChange bool
	statusEntries := 0
	for _, it := range items {
		if it.Field != "status_id" {
			continue
		}
		statusEntries++
		if it.Old == float64(env.statusA) && it.New == float64(env.statusB) {
			sawStatusChange = true
		}
	}
	if !sawStatusChange || statusEntries != 2 {
		t.Fatalf("status entries = %d (A to B seen: %t), want the A to B change and the one made by changing list", statusEntries, sawStatusChange)
	}
	for _, action := range []string{"comment.created", "comment.updated", "comment.deleted"} {
		if findActivity(items, action, "") == nil {
			t.Errorf("missing %s", action)
		}
	}

	page, total := fetchActivity(t, tc, tc.Token, taskID, "?limit=2&page=2")
	if len(page) != 2 || int(total) != len(items) {
		t.Fatalf("page 2 = %d items, total %v", len(page), total)
	}
	if !reflect.DeepEqual(page[0], items[2]) || !reflect.DeepEqual(page[1], items[3]) {
		t.Fatalf("page 2 does not continue the list")
	}

	_, outsiderToken := newUser(t, tc, "activity-outsider@example.test")
	if rec := request(t, tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(taskID)+"/activity", outsiderToken, nil); rec.Code != http.StatusForbidden {
		t.Fatalf("outsider: expected 403, got %d", rec.Code)
	}
}
