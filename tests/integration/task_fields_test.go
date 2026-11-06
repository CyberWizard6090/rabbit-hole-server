package integration

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTaskFields_DatesParentAndAssigneesViaAPI(t *testing.T) {
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
	tasksURL := "/api/v1/lists/" + itoa(listID) + "/tasks"

	mk := func(body map[string]any) *httptest.ResponseRecorder {
		body["status_id"] = statusID
		return request(t, tc.Router, http.MethodPost, tasksURL, tc.Token, body)
	}

	if rec := mk(map[string]any{"title": "bad", "start_date": "2026-05-10T00:00:00Z", "due_date": "2026-05-01T00:00:00Z"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("due before start: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = mk(map[string]any{"title": "parent", "start_date": "2026-05-01T00:00:00Z", "due_date": "2026-05-10T00:00:00Z"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create parent: %d: %s", rec.Code, rec.Body.String())
	}
	parentID := responseID(t, rec)
	rec = mk(map[string]any{"title": "child", "parent_id": parentID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create child: %d: %s", rec.Code, rec.Body.String())
	}
	childID := responseID(t, rec)

	patch := func(id uint, body map[string]any) *httptest.ResponseRecorder {
		return request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(id), tc.Token, body)
	}

	if rec := patch(parentID, map[string]any{"parent_id": childID}); rec.Code != http.StatusBadRequest {
		t.Fatalf("cycle: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := patch(parentID, map[string]any{"parent_id": parentID}); rec.Code != http.StatusBadRequest {
		t.Fatalf("self parent: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	if rec := patch(parentID, map[string]any{"due_date": nil}); rec.Code != http.StatusOK {
		t.Fatalf("clear due: %d: %s", rec.Code, rec.Body.String())
	}
	var due, start *string
	row := tc.DB.Table("tasks").Select("due_date::text, start_date::text").Where("id = ?", parentID).Row()
	if err := row.Scan(&due, &start); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if due != nil || start == nil {
		t.Fatalf("due=%v start=%v, want due cleared and start kept", due, start)
	}
	if rec := patch(childID, map[string]any{"parent_id": nil}); rec.Code != http.StatusOK {
		t.Fatalf("clear parent: %d: %s", rec.Code, rec.Body.String())
	}

	if rec := patch(childID, map[string]any{"assignee_ids": []uint{tc.UserID}}); rec.Code != http.StatusOK {
		t.Fatalf("assign: %d: %s", rec.Code, rec.Body.String())
	}
	count := func() int64 {
		var n int64
		tc.DB.Table("task_assignees").Where("task_id = ?", childID).Count(&n)
		return n
	}
	if n := count(); n != 1 {
		t.Fatalf("assignees = %d, want 1", n)
	}
	if rec := patch(childID, map[string]any{"assignee_ids": []uint{tc.UserID + 9999}}); rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign assignee: expected 400, got %d", rec.Code)
	}
	if rec := patch(childID, map[string]any{"assignee_ids": []uint{}}); rec.Code != http.StatusOK {
		t.Fatalf("unassign: %d: %s", rec.Code, rec.Body.String())
	}
	if n := count(); n != 0 {
		t.Fatalf("assignees = %d, want 0", n)
	}
}

func TestTaskAssigneeMustHaveActiveWorkspaceMembership(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)
	var workspaceID uint
	if err := tc.DB.Table("spaces").Select("workspace_id").Where("id = ?", spaceID).Scan(&workspaceID).Error; err != nil {
		t.Fatal(err)
	}

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token,
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
	}
	statusID := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", tc.Token,
		map[string]any{"title": "assignee membership", "status_id": statusID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	taskID := responseID(t, rec)

	memberID, _ := newUser(t, tc, "removed-assignee@example.test")
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/contacts", tc.Token, map[string]any{"contact_id": memberID}); rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("add contact: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/members", tc.Token,
		map[string]any{"user_id": memberID, "role": "Member"}); rec.Code != http.StatusCreated {
		t.Fatalf("add workspace member: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/workspaces/"+itoa(workspaceID)+"/members/"+itoa(memberID), tc.Token, nil); rec.Code != http.StatusOK {
		t.Fatalf("remove workspace member: %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), tc.Token,
		map[string]any{"assignee_ids": []uint{memberID}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("assign removed member: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
