package integration

import (
	"net/http"
	"reflect"
	"sort"
	"sync"
	"testing"
)

func trTags(t *testing.T, tc *TestContext, taskID uint) []uint {
	t.Helper()
	var ids []uint
	if err := tc.DB.Table("task_tags").Where("task_id = ?", taskID).Order("tag_id").Pluck("tag_id", &ids).Error; err != nil {
		t.Fatalf("read task tags: %v", err)
	}
	return ids
}

func trSetup(t *testing.T, label string) (fcEnv, *TestContext, uint, []uint) {
	t.Helper()
	tc := newTestContext(t)
	env := newFCEnv(t, tc, label)
	tags := make([]uint, 3)
	for i := range tags {
		tags[i] = mustCreate(t, tc, env.owner.token, "/api/v1/spaces/"+itoa(env.owner.spaceID)+"/tags",
			map[string]any{"name": "tag" + itoa(uint(i)), "color": "#112233"})
	}
	taskID := mustCreate(t, tc, env.owner.token, rfTasksURL(env), map[string]any{
		"title": "Tagged", "status_id": env.statusID, "tag_ids": tags[:2],
	})
	return env, tc, taskID, tags
}

func TestTaskPatch_RemoveTagIDs(t *testing.T) {
	env, tc, taskID, tags := trSetup(t, "trremove")
	url := "/api/v1/tasks/" + itoa(taskID)

	rec := request(t, tc.Router, http.MethodPatch, url, env.owner.token, map[string]any{"remove_tag_ids": []uint{tags[0]}})
	if rec.Code != http.StatusOK {
		t.Fatalf("remove tag: %d: %s", rec.Code, rec.Body.String())
	}
	if got := trTags(t, tc, taskID); !reflect.DeepEqual(got, []uint{tags[1]}) {
		t.Fatalf("tags after removal = %v, want [%d]", got, tags[1])
	}
	returned, _ := dataObject(t, rec)["tags"].([]any)
	if len(returned) != 1 || returned[0].(map[string]any)["id"] != float64(tags[1]) {
		t.Fatalf("response tags = %v", returned)
	}

	items, _ := fetchActivity(t, tc, env.owner.token, taskID, "")
	entry := findActivity(items, "task.updated", "tag_ids")
	if entry == nil || !reflect.DeepEqual(entry.New, []any{float64(tags[1])}) || !reflect.DeepEqual(entry.Old, []any{float64(tags[0]), float64(tags[1])}) {
		t.Fatalf("tag activity entry = %+v", entry)
	}

	rec = request(t, tc.Router, http.MethodPatch, url, env.owner.token, map[string]any{"remove_tag_ids": []uint{tags[0], 999999}})
	if rec.Code != http.StatusOK {
		t.Fatalf("removing absent tags must be a no-op: %d: %s", rec.Code, rec.Body.String())
	}
	if got := trTags(t, tc, taskID); !reflect.DeepEqual(got, []uint{tags[1]}) {
		t.Fatalf("tags after no-op removal = %v", got)
	}

	rec = request(t, tc.Router, http.MethodPatch, url, env.owner.token,
		map[string]any{"remove_tag_ids": []uint{tags[1]}, "add_tag_ids": []uint{tags[2]}})
	if rec.Code != http.StatusOK {
		t.Fatalf("add and remove together: %d: %s", rec.Code, rec.Body.String())
	}
	if got := trTags(t, tc, taskID); !reflect.DeepEqual(got, []uint{tags[2]}) {
		t.Fatalf("tags after add+remove = %v, want [%d]", got, tags[2])
	}

	rec = request(t, tc.Router, http.MethodPatch, url, env.owner.token,
		map[string]any{"remove_tag_ids": []uint{tags[2]}, "add_tag_ids": []uint{tags[2]}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("same tag added and removed: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPatch, url, env.owner.token, map[string]any{"remove_tag_ids": []uint{tags[2]}})
	if rec.Code != http.StatusOK || len(trTags(t, tc, taskID)) != 0 {
		t.Fatalf("remove last tag: %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskPatch_RemoveTagIDsAuthorization(t *testing.T) {
	env, tc, taskID, tags := trSetup(t, "trauth")
	_, outsider := newUser(t, tc, "tag-outsider@example.test")
	rec := request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), outsider,
		map[string]any{"remove_tag_ids": []uint{tags[0]}})
	if rec.Code != http.StatusForbidden && rec.Code != http.StatusNotFound {
		t.Fatalf("outsider removal: expected 403/404, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := trTags(t, tc, taskID); len(got) != 2 {
		t.Fatalf("outsider changed tags: %v", got)
	}
	rec = request(t, tc.Router, http.MethodPatch, "/api/v1/tasks/"+itoa(taskID), env.owner.token,
		map[string]any{"remove_tag_ids": []uint{0}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("zero tag id: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTaskPatch_ConcurrentTagAddAndRemoveKeepBothChanges(t *testing.T) {
	env, tc, taskID, tags := trSetup(t, "trrace")
	url := "/api/v1/tasks/" + itoa(taskID)
	for round := 0; round < 8; round++ {
		if err := tc.DB.Exec("DELETE FROM task_tags WHERE task_id = ?", taskID).Error; err != nil {
			t.Fatal(err)
		}
		if err := tc.DB.Exec("INSERT INTO task_tags (task_id, tag_id) VALUES (?, ?), (?, ?)",
			taskID, tags[0], taskID, tags[1]).Error; err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		codes := make([]int, 2)
		bodies := []map[string]any{
			{"add_tag_ids": []uint{tags[2]}},
			{"remove_tag_ids": []uint{tags[0]}},
		}
		for i := range bodies {
			wg.Add(1)
			go func() {
				defer wg.Done()
				codes[i] = request(t, tc.Router, http.MethodPatch, url, env.owner.token, bodies[i]).Code
			}()
		}
		wg.Wait()
		if codes[0] != http.StatusOK || codes[1] != http.StatusOK {
			t.Fatalf("round %d: status codes %v", round, codes)
		}
		got := trTags(t, tc, taskID)
		want := []uint{tags[1], tags[2]}
		sort.Slice(want, func(i, j int) bool { return want[i] < want[j] })
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: tags = %v, want %v", round, got, want)
		}
	}
}
