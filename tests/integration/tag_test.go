package integration

import (
	"net/http"
	"testing"

	"rabbit-hole-server/internal/persistence"
)

func TestTag_DuplicateNameIsRejected(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)

	body := map[string]any{"name": "Important", "color": "#ff0000"}

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first tag: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token, body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate tag: expected 409, got %d: %s", rec.Code, rec.Body.String())
	}

	errorBody := decodeJSON(t, rec)
	errorPayload, ok := errorBody["error"].(map[string]any)
	if !ok || errorPayload["code"] != "TAG_NAME_TAKEN" {
		t.Fatalf("duplicate tag: unexpected error payload: %s", rec.Body.String())
	}
}

func TestTag_MergeMovesTaskRelations(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	createTag := func(name string) uint {
		t.Helper()
		rec := request(t, tc.Router, http.MethodPost,
			"/api/v1/spaces/"+itoa(spaceID)+"/tags",
			tc.Token,
			map[string]any{"name": name, "color": "#00ff00"},
		)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create tag %s: %d: %s", name, rec.Code, rec.Body.String())
		}
		return uint(dataObject(t, rec)["id"].(float64))
	}

	sourceID := createTag("Source")
	targetID := createTag("Target")
	rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/lists/"+itoa(listID)+"/statuses",
		tc.Token,
		map[string]any{"name": "Task Status", "color": "#112233", "position": 1, "type": 1, "list_id": listID},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task status: %d: %s", rec.Code, rec.Body.String())
	}
	statusID := uint(dataObject(t, rec)["id"].(float64))

	rec = request(t, tc.Router, http.MethodPost,
		"/api/v1/lists/"+itoa(listID)+"/tasks",
		tc.Token,
		map[string]any{
			"title":     "Tagged task",
			"status_id": statusID,
			"tag_ids":   []uint{sourceID},
		},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	taskID := uint(dataObject(t, rec)["id"].(float64))

	rec = request(t, tc.Router, http.MethodPost,
		"/api/v1/spaces/"+itoa(spaceID)+"/tags/merge",
		tc.Token,
		map[string]any{"source_tag_id": sourceID, "target_tag_id": targetID},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("merge tags: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var source persistence.Tag
	if err := tc.DB.First(&source, sourceID).Error; err == nil {
		t.Fatalf("source tag still exists")
	}

	var count int64
	if err := tc.DB.Table("task_tags").Where("task_id = ? AND tag_id = ?", taskID, targetID).Count(&count).Error; err != nil {
		t.Fatalf("count target relation: %v", err)
	}
	if count != 1 {
		t.Fatalf("target relation count = %d, want 1", count)
	}
}

func TestTag_GetByIDIsScopedToItsSpace(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	otherSpaceID := createSpace(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token,
		map[string]any{"name": "Bug", "color": "#ff0000"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create tag: %d: %s", rec.Code, rec.Body.String())
	}
	tagID := responseID(t, rec)

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/tags/"+itoa(tagID), tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get tag: %d: %s", rec.Code, rec.Body.String())
	}
	data := dataObject(t, rec)
	if data["name"] != "Bug" || data["usage_count"] != float64(0) {
		t.Fatalf("tag = %v, want name Bug with usage_count 0", data)
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(otherSpaceID)+"/tags/"+itoa(tagID), tc.Token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("tag via wrong space: expected 404, got %d: %s", rec.Code, rec.Body.String())
	}

	_, strangerToken := newUser(t, tc, "tag-stranger@example.test")
	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/tags/"+itoa(tagID), strangerToken, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("stranger: expected 403, got %d", rec.Code)
	}

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/tags/99999", tc.Token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing tag: expected 404, got %d", rec.Code)
	}
}

func TestTag_ListReportsUsageCountsPerTag(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	createTag := func(name string) uint {
		t.Helper()
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token,
			map[string]any{"name": name, "color": "#00ff00"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create tag %s: %d: %s", name, rec.Code, rec.Body.String())
		}
		return responseID(t, rec)
	}
	busy, light, unused := createTag("busy"), createTag("light"), createTag("unused")

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token,
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
	}
	statusID := responseID(t, rec)

	for i, tags := range [][]uint{{busy, light}, {busy}, {busy}} {
		rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", tc.Token,
			map[string]any{"title": "task", "status_id": statusID, "tag_ids": tags})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create task %d: %d: %s", i, rec.Code, rec.Body.String())
		}
	}

	otherSpace := createSpace(t, tc)
	otherList := createList(t, tc, otherSpace)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/spaces/"+itoa(otherSpace)+"/tags", tc.Token,
		map[string]any{"name": "busy", "color": "#00ff00"})
	otherTag := responseID(t, rec)
	rec = request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(otherList)+"/statuses", tc.Token,
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": otherList})
	otherStatus := responseID(t, rec)
	request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(otherList)+"/tasks", tc.Token,
		map[string]any{"title": "other", "status_id": otherStatus, "tag_ids": []uint{otherTag}})

	rec = request(t, tc.Router, http.MethodGet, "/api/v1/spaces/"+itoa(spaceID)+"/tags", tc.Token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tags: %d: %s", rec.Code, rec.Body.String())
	}
	got := map[uint]float64{}
	for _, item := range decodeJSON(t, rec)["data"].([]any) {
		m := item.(map[string]any)
		got[uint(m["id"].(float64))] = m["usage_count"].(float64)
	}
	want := map[uint]float64{busy: 3, light: 1, unused: 0}
	if len(got) != len(want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("usage_count for tag %d = %v, want %v", id, got[id], n)
		}
	}
}
