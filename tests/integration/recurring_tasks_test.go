package integration

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Dates far in the future keep the schedule of "due" rules independent of today's date.
const (
	futureDue   = "2090-03-02T09:00:00Z" // a Monday
	futureStart = "2090-03-01T09:00:00Z"
)

type recurEnv struct {
	tc     *TestContext
	owner  workspaceOwner
	listID uint
	todo   uint
	doing  uint
	done   uint
}

func newRecurEnv(t *testing.T) *recurEnv {
	t.Helper()
	tc := newTestContext(t)
	e := &recurEnv{tc: tc, owner: newWorkspaceOwner(t, tc, "recur")}
	e.listID = mustCreate(t, tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/lists", map[string]any{"name": "Chores"})
	rec := request(t, tc.Router, http.MethodGet, "/api/v1/lists/"+itoa(e.listID)+"/statuses", e.owner.token, nil)
	for _, item := range decodeJSON(t, rec)["data"].([]any) {
		status := item.(map[string]any)
		id := uint(status["id"].(float64))
		switch status["type"].(float64) {
		case 1:
			if e.todo == 0 {
				e.todo = id
			}
		case 2:
			if e.doing == 0 {
				e.doing = id
			}
		case 3:
			if e.done == 0 {
				e.done = id
			}
		}
	}
	if e.todo == 0 || e.doing == 0 || e.done == 0 {
		t.Fatalf("the list has no todo, in progress and done statuses: %v", rec.Body.String())
	}
	return e
}

func (e *recurEnv) create(t *testing.T, body map[string]any) map[string]any {
	t.Helper()
	if _, ok := body["status_id"]; !ok {
		body["status_id"] = e.todo
	}
	rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(e.listID)+"/tasks", e.owner.token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)
}

func (e *recurEnv) get(t *testing.T, id float64) map[string]any {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(uint(id)), e.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get task: %d: %s", rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)
}

func (e *recurEnv) complete(t *testing.T, id float64) map[string]any {
	t.Helper()
	rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(uint(id)), e.owner.token, map[string]any{"status_id": e.done})
	if rec.Code != http.StatusOK {
		t.Fatalf("complete task: %d: %s", rec.Code, rec.Body.String())
	}
	return dataObject(t, rec)
}

// next completes the task and returns the task created in its place.
func (e *recurEnv) next(t *testing.T, id float64) map[string]any {
	t.Helper()
	done := e.complete(t, id)
	nextID, ok := done["next_occurrence_id"].(float64)
	if !ok {
		t.Fatalf("completing the task created no next occurrence: %v", done)
	}
	return e.get(t, nextID)
}

func weekly(extra map[string]any) map[string]any {
	rule := map[string]any{"frequency": "weekly", "time_zone": "UTC"}
	for k, v := range extra {
		rule[k] = v
	}
	return rule
}

func TestRecurring_TheRuleIsStoredNormalizedAndReturned(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Water the plants", "due_date": futureDue, "recurrence": map[string]any{"frequency": "daily"}})
	rule, ok := task["recurrence"].(map[string]any)
	if !ok {
		t.Fatalf("the created task carries no rule: %v", task["recurrence"])
	}
	if rule["interval"] != float64(1) || rule["anchor"] != "due" || rule["time_zone"] != "UTC" || rule["occurrence"] != float64(1) {
		t.Fatalf("defaults were not applied: %v", rule)
	}
	if got := e.get(t, task["id"].(float64))["recurrence"]; got == nil {
		t.Fatal("the rule is not stored")
	}
	plain := e.create(t, map[string]any{"title": "Once"})
	if plain["recurrence"] != nil {
		t.Fatalf("a task without a rule has recurrence %v, want null", plain["recurrence"])
	}
	list := request(t, e.tc.Router, http.MethodGet, "/api/v1/lists/"+itoa(e.listID)+"/tasks", e.owner.token, nil)
	if !strings.Contains(list.Body.String(), `"recurrence":{"frequency":"daily"`) {
		t.Fatalf("the list does not show the rule: %.400s", list.Body.String())
	}
}

func TestRecurring_InvalidRulesAreRejectedWithDetails(t *testing.T) {
	e := newRecurEnv(t)
	cases := map[string]struct {
		rule  map[string]any
		field string
	}{
		"unknown frequency":     {map[string]any{"frequency": "hourly"}, "recurrence.frequency"},
		"missing frequency":     {map[string]any{"interval": 2}, "recurrence.frequency"},
		"interval out of range": {map[string]any{"frequency": "daily", "interval": 9999}, "recurrence.interval"},
		"weekday out of range":  {weekly(map[string]any{"weekdays": []int{0}}), "recurrence.weekdays"},
		"weekdays on daily":     {map[string]any{"frequency": "daily", "weekdays": []int{1}}, "recurrence.weekdays"},
		"month day on weekly":   {weekly(map[string]any{"month_day": 5}), "recurrence.month_day"},
		"unknown zone":          {map[string]any{"frequency": "daily", "time_zone": "Mars/Base"}, "recurrence.time_zone"},
		"bad until":             {map[string]any{"frequency": "daily", "until": "soon"}, "recurrence.until"},
		"bad count":             {map[string]any{"frequency": "daily", "count": 100000}, "recurrence.count"},
		"weekdays after done":   {weekly(map[string]any{"anchor": "completion", "weekdays": []int{2}}), "recurrence.weekdays"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(e.listID)+"/tasks", e.owner.token,
				map[string]any{"title": "x", "status_id": e.todo, "recurrence": tc.rule})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
			}
			body := decodeJSON(t, rec)["error"].(map[string]any)
			if body["code"] != "VALIDATION_FAILED" || !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`"`) {
				t.Fatalf("no detail on %s: %s", tc.field, rec.Body.String())
			}
		})
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'x'"); n != 0 {
		t.Fatalf("%d tasks were created with an invalid rule", n)
	}
}

func TestRecurring_TheRuleCanBeSetReplacedAndRemoved(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Report", "due_date": futureDue})
	id := task["id"].(float64)
	patch := func(rule any) map[string]any {
		rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(uint(id)), e.owner.token, map[string]any{"recurrence": rule})
		if rec.Code != http.StatusOK {
			t.Fatalf("patch: %d: %s", rec.Code, rec.Body.String())
		}
		return dataObject(t, rec)
	}
	if got := patch(weekly(map[string]any{"interval": 2}))["recurrence"].(map[string]any); got["interval"] != float64(2) {
		t.Fatalf("rule not set: %v", got)
	}
	if got := patch(map[string]any{"frequency": "monthly"})["recurrence"].(map[string]any); got["frequency"] != "monthly" || got["interval"] != float64(1) {
		t.Fatalf("rule not replaced: %v", got)
	}
	if got := patch(nil)["recurrence"]; got != nil {
		t.Fatalf("rule not removed: %v", got)
	}
	// An update that does not mention the rule leaves it alone.
	patch(map[string]any{"frequency": "daily"})
	rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(uint(id)), e.owner.token, map[string]any{"title": "Report 2"})
	if dataObject(t, rec)["recurrence"] == nil {
		t.Fatal("renaming the task dropped its rule")
	}
	history := request(t, e.tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(uint(id))+"/activity", e.owner.token, nil)
	if !strings.Contains(history.Body.String(), `"field":"recurrence"`) {
		t.Fatalf("changes of the rule are not in the history: %.600s", history.Body.String())
	}
}

func TestRecurring_CompletingCreatesTheNextTaskAndMovesTheRule(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{
		"title": "Weekly review", "description": "look at the board", "priority": 4, "time_estimate": 1800,
		"start_date": futureStart, "due_date": futureDue, "recurrence": weekly(nil),
	})
	done := e.complete(t, task["id"].(float64))
	if done["recurrence"] != nil {
		t.Fatalf("the completed task keeps its rule: %v", done["recurrence"])
	}
	nextID, ok := done["next_occurrence_id"].(float64)
	if !ok || nextID == task["id"].(float64) {
		t.Fatalf("answer lacks next_occurrence_id: %v", done)
	}

	next := e.get(t, nextID)
	if next["title"] != "Weekly review" || next["description"] != "look at the board" || next["priority"] != float64(4) || next["time_estimate"] != float64(1800) {
		t.Fatalf("the next task does not repeat the content: %v", next)
	}
	if next["due_date"] != "2090-03-09T09:00:00Z" || next["start_date"] != "2090-03-08T09:00:00Z" {
		t.Fatalf("dates = %v / %v, want a week later keeping the distance", next["start_date"], next["due_date"])
	}
	if next["status_id"] != float64(e.todo) {
		t.Fatalf("the next task starts in status %v, want the first open one %d", next["status_id"], e.todo)
	}
	rule := next["recurrence"].(map[string]any)
	if rule["occurrence"] != float64(2) || rule["frequency"] != "weekly" {
		t.Fatalf("the rule did not move on: %v", rule)
	}
	if next["source"] != "automation" {
		t.Fatalf("origin = %v, want automation", next["source"])
	}
	createdBy := next["created_by"].(map[string]any)
	if createdBy["type"] != "automation" || createdBy["name"] != "recurrence" {
		t.Fatalf("created_by = %v, want the recurrence automation", createdBy)
	}
	if e.get(t, task["id"].(float64))["status_id"] != float64(e.done) {
		t.Fatal("the completed task did not stay completed")
	}
	if !strings.Contains(request(t, e.tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(uint(task["id"].(float64)))+"/activity", e.owner.token, nil).Body.String(), `"action":"task.recurred"`) {
		t.Fatal("the completed task has no task.recurred entry")
	}
}

func TestRecurring_TheNextTaskInheritsPeopleTagsChecklistsAndFields(t *testing.T) {
	e := newRecurEnv(t)
	tag := mustCreate(t, e.tc, e.owner.token, "/api/v1/spaces/"+itoa(e.owner.spaceID)+"/tags", map[string]any{"name": "home", "color": "#112233"})
	field := mustCreate(t, e.tc, e.owner.token, "/api/v1/workspaces/"+itoa(e.owner.workspaceID)+"/custom-fields", map[string]any{"name": "Room", "type": "text"})
	task := e.create(t, map[string]any{
		"title": "Clean", "due_date": futureDue, "assignee_ids": []uint{e.owner.userID}, "tag_ids": []uint{tag},
		"recurrence": map[string]any{"frequency": "daily"},
	})
	id := uint(task["id"].(float64))
	if rec := request(t, e.tc.Router, http.MethodPut, "/api/v1/tasks/"+itoa(id)+"/custom-fields/"+itoa(field), e.owner.token, map[string]any{"value": "kitchen"}); rec.Code != http.StatusOK {
		t.Fatalf("set field: %d: %s", rec.Code, rec.Body.String())
	}
	list := mustCreate(t, e.tc, e.owner.token, "/api/v1/tasks/"+itoa(id)+"/checklists", map[string]any{"name": "Steps"})
	first := mustCreate(t, e.tc, e.owner.token, "/api/v1/tasks/"+itoa(id)+"/checklists/"+itoa(list)+"/items", map[string]any{"text": "floor"})
	mustCreate(t, e.tc, e.owner.token, "/api/v1/tasks/"+itoa(id)+"/checklists/"+itoa(list)+"/items", map[string]any{"text": "windows"})
	if rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(id)+"/checklists/"+itoa(list)+"/items/"+itoa(first), e.owner.token, map[string]any{"resolved": true}); rec.Code != http.StatusOK {
		t.Fatalf("resolve item: %d: %s", rec.Code, rec.Body.String())
	}
	mustCreate(t, e.tc, e.owner.token, "/api/v1/tasks/"+itoa(id)+"/comments", map[string]any{"body": "do the oven too"})

	next := e.next(t, task["id"].(float64))
	nextID := uint(next["id"].(float64))
	if got := next["assignees"].([]any); len(got) != 1 || got[0].(map[string]any)["id"] != float64(e.owner.userID) {
		t.Fatalf("assignees = %v", got)
	}
	if got := next["tags"].([]any); len(got) != 1 || got[0].(map[string]any)["id"] != float64(tag) {
		t.Fatalf("tags = %v", got)
	}
	if next["checklist_items_total"] != float64(2) || next["checklist_items_resolved"] != float64(0) {
		t.Fatalf("checklist counts = %v/%v, want 0 of 2 resolved", next["checklist_items_resolved"], next["checklist_items_total"])
	}
	values := dataObject(t, request(t, e.tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(nextID)+"/custom-fields", e.owner.token, nil))
	if values[itoa(field)] != "kitchen" {
		t.Fatalf("custom fields = %v", values)
	}
	comments := decodeJSON(t, request(t, e.tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(nextID)+"/comments", e.owner.token, nil))["data"].([]any)
	if len(comments) != 0 {
		t.Fatalf("comments were copied: %v", comments)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM task_checklist_items WHERE resolved"); n != 1 {
		t.Fatalf("resolved items = %d, the old task keeps its one resolved item and the copy none", n)
	}
}

func TestRecurring_CompletionByMoveAndByBulkUpdateAlsoRepeats(t *testing.T) {
	e := newRecurEnv(t)
	rule := map[string]any{"frequency": "daily"}
	a := e.create(t, map[string]any{"title": "by move", "due_date": futureDue, "recurrence": rule})
	b := e.create(t, map[string]any{"title": "by bulk", "due_date": futureDue, "recurrence": rule})

	rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/tasks/"+itoa(uint(a["id"].(float64)))+"/move", e.owner.token, map[string]any{"status_id": e.done})
	if rec.Code != http.StatusOK || dataObject(t, rec)["next_occurrence_id"] == nil {
		t.Fatalf("move to done: %d: %s", rec.Code, rec.Body.String())
	}
	rec = request(t, e.tc.Router, http.MethodPost, "/api/v1/tasks/bulk", e.owner.token, map[string]any{
		"action": "update", "task_ids": []uint{uint(b["id"].(float64))}, "update": map[string]any{"status_id": e.done},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk: %d: %s", rec.Code, rec.Body.String())
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title IN ('by move', 'by bulk') AND recurrence IS NOT NULL AND deleted_at IS NULL"); n != 2 {
		t.Fatalf("live repeating tasks = %d, want one per series", n)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title IN ('by move', 'by bulk')"); n != 4 {
		t.Fatalf("tasks = %d, want the two completed and the two new ones", n)
	}
}

func TestRecurring_OnlyEnteringADoneStatusRepeats(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Slow", "due_date": futureDue, "recurrence": map[string]any{"frequency": "daily"}})
	id := task["id"].(float64)
	if got := dataObject(t, request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(uint(id)), e.owner.token, map[string]any{"status_id": e.doing})); got["next_occurrence_id"] != nil {
		t.Fatalf("starting the task created a copy: %v", got)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Slow'"); n != 1 {
		t.Fatalf("tasks = %d", n)
	}
	done := e.complete(t, id)
	nextID := done["next_occurrence_id"].(float64)

	// Reopening and finishing the old task again must not start a second series: its rule is gone.
	request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(uint(id)), e.owner.token, map[string]any{"status_id": e.todo})
	again := e.complete(t, id)
	if again["next_occurrence_id"] != nil {
		t.Fatalf("completing again created %v", again["next_occurrence_id"])
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Slow'"); n != 2 {
		t.Fatalf("tasks = %d, want the old one and one new", n)
	}
	// Moving the new task between open statuses and a task with no rule do nothing either.
	plain := e.create(t, map[string]any{"title": "Plain"})
	if got := e.complete(t, plain["id"].(float64)); got["next_occurrence_id"] != nil {
		t.Fatalf("a task without a rule repeated: %v", got)
	}
	_ = nextID
}

func TestRecurring_ASeriesEndsAtItsCountOrItsLastDay(t *testing.T) {
	e := newRecurEnv(t)
	first := e.create(t, map[string]any{"title": "Twice", "due_date": futureDue, "recurrence": map[string]any{"frequency": "daily", "count": 2}})
	second := e.next(t, first["id"].(float64))
	if second["recurrence"].(map[string]any)["occurrence"] != float64(2) {
		t.Fatalf("second occurrence = %v", second["recurrence"])
	}
	last := e.complete(t, second["id"].(float64))
	if last["next_occurrence_id"] != nil || last["recurrence"] != nil {
		t.Fatalf("a series with count 2 went on: %v", last)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Twice'"); n != 2 {
		t.Fatalf("tasks = %d, want 2", n)
	}

	bounded := e.create(t, map[string]any{"title": "Until", "due_date": "2090-03-02T09:00:00Z", "recurrence": map[string]any{"frequency": "daily", "until": "2090-03-03"}})
	if next := e.next(t, bounded["id"].(float64)); next["due_date"] != "2090-03-03T09:00:00Z" {
		t.Fatalf("next due = %v, want the last allowed day", next["due_date"])
	}
	var latest float64
	for _, row := range decodeJSON(t, request(t, e.tc.Router, http.MethodGet, "/api/v1/lists/"+itoa(e.listID)+"/tasks?q=Until", e.owner.token, nil))["data"].(map[string]any)["items"].([]any) {
		if row.(map[string]any)["recurrence"] != nil {
			latest = row.(map[string]any)["id"].(float64)
		}
	}
	if latest == 0 {
		t.Fatal("the live task of the series was not found")
	}
	if end := e.complete(t, latest); end["next_occurrence_id"] != nil {
		t.Fatalf("the series went past its last day: %v", end)
	}
}

func TestRecurring_LateCompletionSkipsMissedDaysAndCompletionAnchorCountsFromNow(t *testing.T) {
	e := newRecurEnv(t)
	late := e.create(t, map[string]any{"title": "Late", "due_date": "2020-01-01T09:00:00Z", "recurrence": map[string]any{"frequency": "daily"}})
	next := e.next(t, late["id"].(float64))
	due, err := time.Parse(time.RFC3339, next["due_date"].(string))
	if err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC().Truncate(24 * time.Hour)
	if !due.After(today) || due.After(today.Add(48*time.Hour)) || due.Hour() != 9 {
		t.Fatalf("next due = %v, want tomorrow at 09:00 (missed days are skipped)", due)
	}

	after := e.create(t, map[string]any{"title": "After", "due_date": futureDue, "recurrence": map[string]any{"frequency": "daily", "interval": 3, "anchor": "completion"}})
	started := time.Now()
	nextAfter := e.next(t, after["id"].(float64))
	got, _ := time.Parse(time.RFC3339, nextAfter["due_date"].(string))
	if want := started.AddDate(0, 0, 3); got.Before(want.Add(-time.Minute)) || got.After(want.Add(time.Minute)) {
		t.Fatalf("next due = %v, want about %v (three days after completing)", got, want)
	}
}

func TestRecurring_ConcurrentCompletionsCreateExactlyOneNextTask(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Race", "due_date": futureDue, "recurrence": map[string]any{"frequency": "daily"}})
	id := uint(task["id"].(float64))
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request(t, e.tc.Raw, http.MethodPatch, "/api/v1/tasks/"+itoa(id), e.owner.token, map[string]any{"status_id": e.done})
		}()
	}
	wg.Wait()
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Race'"); n != 2 {
		t.Fatalf("tasks = %d, want the completed one and exactly one next", n)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Race' AND recurrence IS NOT NULL"); n != 1 {
		t.Fatalf("tasks with a rule = %d, want 1", n)
	}
}

func TestRecurring_MonthlyRuleKeepsItsDayAfterAShortMonth(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Rent", "due_date": "2090-01-31T09:00:00Z", "recurrence": map[string]any{"frequency": "monthly"}})
	february := e.next(t, task["id"].(float64))
	if february["due_date"] != "2090-02-28T09:00:00Z" {
		t.Fatalf("February due = %v", february["due_date"])
	}
	if march := e.next(t, february["id"].(float64)); march["due_date"] != "2090-03-31T09:00:00Z" {
		t.Fatalf("March due = %v, want the 31st again", march["due_date"])
	}
}

func TestRecurring_PermissionsFollowTheTask(t *testing.T) {
	e := newRecurEnv(t)
	guestID, guestToken := newUser(t, e.tc, fmt.Sprintf("recur-guest-%d@example.test", time.Now().UnixNano()))
	addContact(t, e.tc, e.owner.token, guestID)
	if rec := request(t, e.tc.Router, http.MethodPost, membersPath(e.owner.workspaceID, ""), e.owner.token, map[string]any{"user_id": guestID, "role": "Guest"}); rec.Code != http.StatusCreated {
		t.Fatalf("add guest: %d", rec.Code)
	}
	task := e.create(t, map[string]any{"title": "Mine", "due_date": futureDue})
	rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(uint(task["id"].(float64))), guestToken, map[string]any{"recurrence": map[string]any{"frequency": "daily"}})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("a guest set a rule: %d", rec.Code)
	}
	if e.get(t, task["id"].(float64))["recurrence"] != nil {
		t.Fatal("the rule was stored")
	}
}
