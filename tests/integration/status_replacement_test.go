package integration

import (
	"net/http"
	"sync"
	"testing"
)

func srStatus(t *testing.T, tc *TestContext, token string, listID uint, name string) uint {
	t.Helper()
	return mustCreate(t, tc, token, "/api/v1/lists/"+itoa(listID)+"/statuses",
		map[string]any{"name": name, "color": "#112233", "type": 1, "list_id": listID})
}

func srStatusURL(listID, statusID uint) string {
	return "/api/v1/lists/" + itoa(listID) + "/statuses/" + itoa(statusID)
}

func srTaskStatus(t *testing.T, tc *TestContext, taskID uint) uint {
	t.Helper()
	var id uint
	if err := tc.DB.Unscoped().Table("tasks").Select("status_id").Where("id = ?", taskID).Scan(&id).Error; err != nil {
		t.Fatal(err)
	}
	return id
}

func TestStatusDelete_ReplacementStatusIDMovesTasks(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "srmove")
	token := env.owner.token
	from := srStatus(t, tc, token, env.listID, "From")
	to := srStatus(t, tc, token, env.listID, "To")
	foreign := srStatus(t, tc, token, mustCreate(t, tc, token,
		"/api/v1/spaces/"+itoa(env.owner.spaceID)+"/lists", map[string]any{"name": "Other"}), "Foreign")

	parent := mustCreate(t, tc, token, rfTasksURL(env), map[string]any{"title": "parent", "status_id": from})
	child := mustCreate(t, tc, token, rfTasksURL(env), map[string]any{"title": "child", "status_id": from, "parent_id": parent})
	live := mustCreate(t, tc, token, rfTasksURL(env), map[string]any{"title": "live", "status_id": from})
	other := mustCreate(t, tc, token, rfTasksURL(env), map[string]any{"title": "other", "status_id": to})
	if rec := request(t, tc.Router, http.MethodDelete, "/api/v1/tasks/"+itoa(parent), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete parent: %d: %s", rec.Code, rec.Body.String())
	}

	rec := request(t, tc.Router, http.MethodDelete, srStatusURL(env.listID, from), token, nil)
	if rec.Code != http.StatusConflict || fcErrorBody(t, rec)["code"] != "STATUS_IN_USE" {
		t.Fatalf("without replacement: expected 409 STATUS_IN_USE, got %d: %s", rec.Code, rec.Body.String())
	}

	for name, replacement := range map[string]string{
		"itself": itoa(from), "other list": itoa(foreign), "missing": "99999", "zero": "0", "text": "abc",
	} {
		rec := request(t, tc.Router, http.MethodDelete, srStatusURL(env.listID, from)+"?replacement_status_id="+replacement, token, nil)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: expected 400, got %d: %s", name, rec.Code, rec.Body.String())
		}
	}
	if srTaskStatus(t, tc, live) != from {
		t.Fatal("an invalid replacement changed tasks")
	}

	rec = request(t, tc.Router, http.MethodDelete, srStatusURL(env.listID, from)+"?replacement_status_id="+itoa(to), token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("with replacement_status_id: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	for name, id := range map[string]uint{"live": live, "child of a deleted parent": child, "deleted parent": parent, "untouched": other} {
		if got := srTaskStatus(t, tc, id); got != to {
			t.Errorf("%s task status = %d, want %d", name, got, to)
		}
	}
	var alive int64
	tc.DB.Table("task_statuses").Where("id = ? AND deleted_at IS NULL", from).Count(&alive)
	if alive != 0 {
		t.Fatal("status still alive")
	}
}

func TestStatusDelete_ReplacementKeepsWorkingUnderConcurrentTaskCreation(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "srrace")
	token := env.owner.token
	from := srStatus(t, tc, token, env.listID, "From")
	to := srStatus(t, tc, token, env.listID, "To")
	mustCreate(t, tc, token, rfTasksURL(env), map[string]any{"title": "seed", "status_id": from})

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			request(t, tc.Router, http.MethodPost, rfTasksURL(env), token,
				map[string]any{"title": "racer " + itoa(uint(i)), "status_id": from})
		}()
	}
	var deleteCode int
	wg.Add(1)
	go func() {
		defer wg.Done()
		deleteCode = request(t, tc.Router, http.MethodDelete,
			srStatusURL(env.listID, from)+"?replacement_status_id="+itoa(to), token, nil).Code
	}()
	wg.Wait()
	if deleteCode != http.StatusOK {
		t.Fatalf("delete status: %d", deleteCode)
	}
	var dangling int64
	tc.DB.Table("tasks").Where("status_id = ? AND deleted_at IS NULL", from).Count(&dangling)
	if dangling != 0 {
		t.Fatalf("%d live tasks still point to the deleted status", dangling)
	}
}

func TestStatusDelete_RefusedWhilePublishedFormUsesIt(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "srform")
	token := env.owner.token
	used := srStatus(t, tc, token, env.listID, "Used by form")
	spare := srStatus(t, tc, token, env.listID, "Spare")
	formID, formURL := env.newForm(t, tc, "Status form")
	titleID, _ := fcAddQuestion(t, tc, token, formURL, "Title", nil)

	config := env.taskConfig([]any{fcTitleMapping(titleID)})
	config["task_defaults"] = map[string]any{"priority": "normal", "status_id": used}
	if rec := request(t, tc.Router, http.MethodPut, formURL+"/task-config", token, config); rec.Code != http.StatusOK {
		t.Fatalf("task config: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPost, formURL+"/publish", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
	slug := dataObject(t, request(t, tc.Router, http.MethodGet, formURL, token, nil))["slug"].(string)

	for _, query := range []string{"", "?replacement_status_id=" + itoa(spare)} {
		rec := request(t, tc.Router, http.MethodDelete, srStatusURL(env.listID, used)+query, token, nil)
		if rec.Code != http.StatusConflict {
			t.Fatalf("delete used status %q: expected 409, got %d: %s", query, rec.Code, rec.Body.String())
		}
		body := fcErrorBody(t, rec)
		if body["code"] != "STATUS_USED_BY_FORM" {
			t.Fatalf("code = %v, want STATUS_USED_BY_FORM", body["code"])
		}
		params, _ := body["params"].(map[string]any)
		ids, _ := params["form_ids"].([]any)
		if len(ids) != 1 || ids[0] != float64(formID) {
			t.Fatalf("params = %v, want form_ids [%d]", params, formID)
		}
	}
	var alive int64
	tc.DB.Table("task_statuses").Where("id = ? AND deleted_at IS NULL", used).Count(&alive)
	if alive != 1 {
		t.Fatal("refused delete removed the status")
	}

	config["task_defaults"] = map[string]any{"priority": "normal", "status_id": spare}
	if rec := request(t, tc.Router, http.MethodPut, formURL+"/task-config", token, config); rec.Code != http.StatusOK {
		t.Fatalf("change task config: %d: %s", rec.Code, rec.Body.String())
	}
	rec := request(t, tc.Router, http.MethodDelete, srStatusURL(env.listID, used), token, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("a draft change must not release the published version: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodPost, formURL+"/publish", token, nil); rec.Code != http.StatusOK {
		t.Fatalf("republish: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := request(t, tc.Router, http.MethodDelete, srStatusURL(env.listID, used), token, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete after republish: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = fcSubmit(t, tc, slug, "sr-after", map[string]any{itoa(titleID): "Still works"})
	if rec.Code != http.StatusCreated || dataObject(t, rec)["task_creation_status"] != "succeeded" {
		t.Fatalf("submission after republish: %d: %s", rec.Code, rec.Body.String())
	}
}
