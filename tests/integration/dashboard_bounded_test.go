package integration

import (
	"fmt"
	"net/http"
	"testing"

	"gorm.io/gorm"
)

func TestDashboard_TasksPerListAreBoundedAndCounted(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	big := createList(t, tc, spaceID)
	small := createList(t, tc, spaceID)
	empty := createList(t, tc, spaceID)

	newStatus := func(list uint) uint {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(list)+"/statuses", tc.Token,
			map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": list})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}
	newTask := func(list, status uint, title string, extra map[string]any) uint {
		body := map[string]any{"title": title, "status_id": status}
		for k, v := range extra {
			body[k] = v
		}
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(list)+"/tasks", tc.Token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}

	bigStatus, smallStatus := newStatus(big), newStatus(small)
	_ = empty
	var bigIDs []uint
	for i := 1; i <= 8; i++ {
		extra := map[string]any(nil)
		if i == 8 {
			extra = map[string]any{"new_tag_names": []string{"urgent"}}
		}
		bigIDs = append(bigIDs, newTask(big, bigStatus, fmt.Sprintf("big task %d", i), extra))
	}
	newTask(small, smallStatus, "small task 1", nil)
	newTask(small, smallStatus, "small task 2", nil)

	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(bigIDs[0]), tc.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete task: %d: %s", rec.Code, rec.Body.String())
	}

	dashboard := func(query string) map[uint]map[string]any {
		t.Helper()
		rec := request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/dashboard"+query, tc.Token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("dashboard%s: %d: %s", query, rec.Code, rec.Body.String())
		}
		lists := map[uint]map[string]any{}
		for _, l := range dataObject(t, rec)["lists"].([]any) {
			m := l.(map[string]any)
			id, ok := m["id"]
			if !ok {
				id = m["ID"]
			}
			lists[uint(id.(float64))] = m
		}
		return lists
	}
	taskTitles := func(list map[string]any) []string {
		var out []string
		for _, task := range list["tasks"].([]any) {
			out = append(out, task.(map[string]any)["title"].(string))
		}
		return out
	}

	lists := dashboard("?tasks_per_list=3")
	if got := taskTitles(lists[big]); fmt.Sprint(got) != "[big task 8 big task 7 big task 6]" {
		t.Fatalf("big list tasks = %v, want its 3 newest", got)
	}
	if lists[big]["task_count"] != float64(7) {
		t.Fatalf("big list task_count = %v, want 7 (8 created, 1 deleted)", lists[big]["task_count"])
	}
	if got := taskTitles(lists[small]); len(got) != 2 || lists[small]["task_count"] != float64(2) {
		t.Fatalf("small list = %v / count %v, want both tasks", got, lists[small]["task_count"])
	}
	if tasks, ok := lists[empty]["tasks"].([]any); !ok || len(tasks) != 0 || lists[empty]["task_count"] != float64(0) {
		t.Fatalf("empty list = %v, want [] tasks and task_count 0", lists[empty])
	}

	newest := lists[big]["tasks"].([]any)[0].(map[string]any)
	if tags, _ := newest["tags"].([]any); len(tags) != 1 || tags[0].(map[string]any)["name"] != "urgent" {
		t.Fatalf("newest task tags = %v, want [urgent]", newest["tags"])
	}
	if statuses, _ := lists[big]["statuses"].([]any); len(statuses) != 1 {
		t.Fatalf("statuses missing from the dashboard: %v", lists[big]["statuses"])
	}

	if got := taskTitles(dashboard("")[big]); len(got) != 7 {
		t.Fatalf("default dashboard returned %d tasks for a 7-task list", len(got))
	}

	for _, bad := range []string{"0", "-1", "abc", "201", ""} {
		if bad == "" {
			continue
		}
		rec := request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/dashboard?tasks_per_list="+bad, tc.Token, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("tasks_per_list=%s: expected 400, got %d", bad, rec.Code)
		}
	}
}

func TestDashboard_QueryCountIsIndependentOfListCount(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	for i := 0; i < 12; i++ {
		createList(t, tc, spaceID)
	}

	var queries int
	if err := tc.DB.Callback().Query().After("gorm:query").Register("test:count_queries", func(*gorm.DB) { queries++ }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tc.DB.Callback().Query().Remove("test:count_queries") })

	rec := request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/dashboard", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard: %d: %s", rec.Code, rec.Body.String())
	}
	if queries > 12 {
		t.Fatalf("dashboard ran %d queries for 13 lists; it should stay constant", queries)
	}
}
