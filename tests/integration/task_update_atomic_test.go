package integration

import (
	"net/http"
	"testing"
)

func TestTaskUpdate_FieldsAndTagsAreAppliedAtomically(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token,
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
	}
	statusID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", tc.Token,
		map[string]any{"title": "original", "status_id": statusID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	taskID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token,
		map[string]any{"name": "ok-tag", "color": "#00ff00"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tag: %d: %s", rec.Code, rec.Body.String())
	}
	goodTagID := responseID(t, rec)

	title := func() string {
		t.Helper()
		var got string
		if err := tc.DB.Table("tasks").Select("title").Where("id = ?", taskID).Scan(&got).Error; err != nil {
			t.Fatalf("read title: %v", err)
		}
		return got
	}
	tagLinks := func() int64 {
		t.Helper()
		var n int64
		if err := tc.DB.Table("task_tags").Where("task_id = ?", taskID).Count(&n).Error; err != nil {
			t.Fatalf("count task_tags: %v", err)
		}
		return n
	}

	otherSpaceID := createSpaceIn(t, tc)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(otherSpaceID)+"/tags", tc.Token,
		map[string]any{"name": "foreign-tag", "color": "#ff0000"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create foreign tag: %d: %s", rec.Code, rec.Body.String())
	}
	foreignTagID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), tc.Token,
		map[string]any{"title": "should not stick", "add_tag_ids": []uint{goodTagID, foreignTagID}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update with foreign tag: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := title(); got != "original" {
		t.Fatalf("title = %q after failed update, want it unchanged", got)
	}
	if n := tagLinks(); n != 0 {
		t.Fatalf("task_tags rows = %d after failed update, want 0", n)
	}

	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), tc.Token,
		map[string]any{"title": "updated", "add_tag_ids": []uint{goodTagID}, "add_tag_names": []string{"brand-new"}})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := title(); got != "updated" {
		t.Fatalf("title = %q, want updated", got)
	}
	if n := tagLinks(); n != 2 {
		t.Fatalf("task_tags rows = %d, want 2", n)
	}

	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), tc.Token,
		map[string]any{"add_tag_ids": []uint{goodTagID}})
	if rec.Code != http.StatusOK {
		t.Fatalf("re-add tag: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if n := tagLinks(); n != 2 {
		t.Fatalf("task_tags rows = %d after re-add, want 2", n)
	}
}

func createSpaceIn(t *testing.T, tc *TestContext) uint {
	t.Helper()
	return createSpace(t, tc)
}
