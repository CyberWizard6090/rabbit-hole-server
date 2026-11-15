package integration

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// A date in the past makes a scheduled series due at once; the weekday does not matter for the daily rules below.
const pastDue = "2020-01-06T09:00:00Z"

func scheduled(extra map[string]any) map[string]any {
	rule := map[string]any{"frequency": "daily", "trigger": "schedule"}
	for k, v := range extra {
		rule[k] = v
	}
	return rule
}

func (e *recurEnv) spawnDue(t *testing.T) int {
	t.Helper()
	created, err := e.tc.SpawnRecurring(context.Background())
	if err != nil {
		t.Fatalf("scheduler: %v", err)
	}
	return created
}

func TestRecurringSchedule_RulesAreValidated(t *testing.T) {
	e := newRecurEnv(t)
	cases := map[string]struct {
		body  map[string]any
		field string
	}{
		"no due date":       {map[string]any{"recurrence": scheduled(nil)}, "recurrence.trigger"},
		"unknown trigger":   {map[string]any{"due_date": futureDue, "recurrence": scheduled(map[string]any{"trigger": "hourly"})}, "recurrence.trigger"},
		"completion anchor": {map[string]any{"due_date": futureDue, "recurrence": scheduled(map[string]any{"anchor": "completion"})}, "recurrence.anchor"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			tc.body["title"], tc.body["status_id"] = "x", e.todo
			rec := request(t, e.tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(e.listID)+"/tasks", e.owner.token, tc.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"`+tc.field+`"`) {
				t.Fatalf("status %d, want 400 with %s: %s", rec.Code, tc.field, rec.Body.String())
			}
		})
	}

	task := e.create(t, map[string]any{"title": "Weekly report", "due_date": futureDue, "recurrence": scheduled(nil)})
	if task["recurrence"].(map[string]any)["trigger"] != "schedule" {
		t.Fatalf("the trigger was not stored: %v", task["recurrence"])
	}
	rec := request(t, e.tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(uint(task["id"].(float64))), e.owner.token, map[string]any{"due_date": nil})
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), `"field":"recurrence.trigger"`) {
		t.Fatalf("the due date of a scheduled task was removed: %d %s", rec.Code, rec.Body.String())
	}
	plain := e.create(t, map[string]any{"title": "Daily", "due_date": futureDue, "recurrence": map[string]any{"frequency": "daily"}})
	if plain["recurrence"].(map[string]any)["trigger"] != "completion" {
		t.Fatalf("the default trigger is %v, want completion", plain["recurrence"])
	}
}

func TestRecurringSchedule_TheNextTaskAppearsWithoutCompletion(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Standup notes", "due_date": pastDue, "recurrence": scheduled(nil), "assignee_ids": []uint{e.owner.userID}})
	future := e.create(t, map[string]any{"title": "Not yet", "due_date": futureDue, "recurrence": scheduled(nil)})

	if got := e.spawnDue(t); got != 1 {
		t.Fatalf("created %d tasks, want 1 (only the overdue series)", got)
	}
	old := e.get(t, task["id"].(float64))
	if old["recurrence"] != nil {
		t.Fatalf("the rule stayed on the old task: %v", old["recurrence"])
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Standup notes'"); n != 2 {
		t.Fatalf("tasks = %d, want the old one and one new", n)
	}
	var due time.Time
	if err := e.tc.DB.Raw("SELECT due_date FROM tasks WHERE title = 'Standup notes' AND recurrence IS NOT NULL").Scan(&due).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 9, 0, 0, 0, time.UTC)
	if !due.Equal(tomorrow) {
		t.Fatalf("the next due date is %s, want %s: late series skip the missed days", due, tomorrow)
	}
	var occurrence, assignees int
	e.tc.DB.Raw("SELECT (recurrence ->> 'occurrence')::int FROM tasks WHERE title = 'Standup notes' AND recurrence IS NOT NULL").Scan(&occurrence)
	e.tc.DB.Raw(`SELECT count(*) FROM task_assignees a JOIN tasks t ON t.id = a.task_id
		WHERE t.title = 'Standup notes' AND t.recurrence IS NOT NULL`).Scan(&assignees)
	if occurrence != 2 || assignees != 1 {
		t.Fatalf("occurrence %d, assignees %d; want 2 and 1", occurrence, assignees)
	}
	history := request(t, e.tc.Router, http.MethodGet, "/api/v1/tasks/"+itoa(uint(task["id"].(float64)))+"/activity", e.owner.token, nil)
	if !strings.Contains(history.Body.String(), `"action":"task.recurred"`) {
		t.Fatalf("the old task has no task.recurred entry: %s", history.Body.String())
	}
	if e.get(t, future["id"].(float64))["recurrence"] == nil {
		t.Fatal("a series that is not due lost its rule")
	}

	if got := e.spawnDue(t); got != 0 {
		t.Fatalf("a second run created %d tasks, want 0", got)
	}
}

func TestRecurringSchedule_CompletingDoesNotSpawnEarlyAndTheSeriesStillContinues(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Invoices", "due_date": futureDue, "recurrence": scheduled(nil)})
	done := e.complete(t, task["id"].(float64))
	if done["next_occurrence_id"] != nil {
		t.Fatalf("completing a scheduled task created a task: %v", done["next_occurrence_id"])
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Invoices'"); n != 1 {
		t.Fatalf("tasks = %d, want 1", n)
	}

	// Its due date passes while it is already done: the next task still appears.
	if err := e.tc.DB.Exec("UPDATE tasks SET due_date = ? WHERE title = 'Invoices'", time.Now().Add(-time.Hour)).Error; err != nil {
		t.Fatal(err)
	}
	if got := e.spawnDue(t); got != 1 {
		t.Fatalf("created %d tasks, want 1", got)
	}
	var open int
	e.tc.DB.Raw(`SELECT count(*) FROM tasks WHERE title = 'Invoices' AND recurrence IS NOT NULL AND status_id = ?`, e.todo).Scan(&open)
	if open != 1 {
		t.Fatalf("the new task is not open with the rule: %d", open)
	}
}

func TestRecurringSchedule_TheSeriesEndsAtItsCount(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Once more", "due_date": pastDue, "recurrence": scheduled(map[string]any{"count": 1})})
	if got := e.spawnDue(t); got != 0 {
		t.Fatalf("created %d tasks past the last occurrence", got)
	}
	if e.get(t, task["id"].(float64))["recurrence"] != nil {
		t.Fatal("the finished series kept its rule and would be retried")
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Once more'"); n != 1 {
		t.Fatalf("tasks = %d, want 1", n)
	}
}

func TestRecurringSchedule_ConcurrentSchedulersCreateExactlyOneTask(t *testing.T) {
	e := newRecurEnv(t)
	e.create(t, map[string]any{"title": "Race", "due_date": pastDue, "recurrence": scheduled(nil)})
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := e.tc.SpawnRecurring(context.Background()); err != nil {
				t.Errorf("scheduler: %v", err)
			}
		}()
	}
	wg.Wait()
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Race'"); n != 2 {
		t.Fatalf("tasks = %d, want the old one and exactly one new", n)
	}
	if n := countRows(t, e.tc, "SELECT count(*) FROM tasks WHERE title = 'Race' AND recurrence IS NOT NULL"); n != 1 {
		t.Fatalf("tasks with a rule = %d, want 1", n)
	}
}

func TestRecurringSchedule_DeletedTasksStopTheirSeries(t *testing.T) {
	e := newRecurEnv(t)
	task := e.create(t, map[string]any{"title": "Gone", "due_date": pastDue, "recurrence": scheduled(nil)})
	rec := request(t, e.tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(uint(task["id"].(float64))), e.owner.token, nil)
	if rec.Code != http.StatusNoContent && rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if got := e.spawnDue(t); got != 0 {
		t.Fatalf("a deleted task's series created %d tasks", got)
	}
}
