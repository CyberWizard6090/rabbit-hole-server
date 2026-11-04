package integration

import (
	"net/http"
	"testing"
)

func TestStatusDelete_RefusedWhileTasksUseIt_UnlessReplaced(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)
	otherListID := createList(t, tc, spaceID)

	newStatus := func(list uint, name string) uint {
		t.Helper()
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(list)+"/statuses", tc.Token,
			map[string]any{"name": name, "color": "#112233", "type": 1, "list_id": list})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}
	todo, done, foreign := newStatus(listID, "Todo"), newStatus(listID, "Done"), newStatus(otherListID, "Foreign")

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", tc.Token,
		map[string]any{"title": "a task", "status_id": todo})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: %d: %s", rec.Code, rec.Body.String())
	}
	taskID := responseID(t, rec)
	statusPath := func(id uint) string { return "/api/v1/lists/" + itoa(listID) + "/statuses/" + itoa(id) }
	taskStatus := func() float64 {
		t.Helper()
		var id uint
		tc.DB.Table("tasks").Select("status_id").Where("id = ?", taskID).Scan(&id)
		return float64(id)
	}
	alive := func(id uint) int64 {
		var n int64
		tc.DB.Table("task_statuses").Where("id = ? AND deleted_at IS NULL", id).Count(&n)
		return n
	}

	t.Run("refused with 409 and nothing changes", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodDelete, statusPath(todo), tc.Token, nil)
		if rec.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
		}
		if alive(todo) != 1 || taskStatus() != float64(todo) {
			t.Fatal("status was deleted or task changed despite the refusal")
		}
	})

	t.Run("replacement must be another status of the same list", func(t *testing.T) {
		for name, replacement := range map[string]uint{"itself": todo, "other list": foreign, "missing": 99999} {
			rec := request(t, tc.Router, http.MethodDelete, statusPath(todo)+"?replacement_id="+itoa(replacement), tc.Token, nil)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s: expected 400, got %d: %s", name, rec.Code, rec.Body.String())
			}
		}
		if alive(todo) != 1 || taskStatus() != float64(todo) {
			t.Fatal("invalid replacement must not change anything")
		}
		if rec := request(t, tc.Router, http.MethodDelete, statusPath(todo)+"?replacement_id=abc", tc.Token, nil); rec.Code != http.StatusBadRequest {
			t.Errorf("non-numeric replacement_id: expected 400, got %d", rec.Code)
		}
	})

	t.Run("with a valid replacement tasks move and the status is deleted", func(t *testing.T) {
		rec := request(t, tc.Router, http.MethodDelete, statusPath(todo)+"?replacement_id="+itoa(done), tc.Token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if alive(todo) != 0 || taskStatus() != float64(done) {
			t.Fatalf("status alive=%d, task status=%v; want deleted and task moved to %d", alive(todo), taskStatus(), done)
		}
	})

	t.Run("an unused status deletes without a replacement", func(t *testing.T) {
		unused := newStatus(listID, "Unused")
		if rec := request(t, tc.Router, http.MethodDelete, statusPath(unused), tc.Token, nil); rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
