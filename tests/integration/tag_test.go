package integration

import (
	"net/http"
	"testing"

	"rabbit-hole-server/internal/domain"
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
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("duplicate tag: expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTag_MergeMovesTaskRelations(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	var statuses []domain.TaskStatus
	if err := tc.DB.Where("space_id = ?", spaceID).
		Order("position ASC").
		Find(&statuses).Error; err != nil {
		t.Fatalf("statuses: %v", err)
	}

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
		"/api/v1/lists/"+itoa(listID)+"/tasks",
		tc.Token,
		map[string]any{
			"title":     "Tagged task",
			"status_id": statuses[0].ID,
			"tag_ids":   []uint{sourceID},
		},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create task: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	taskID := uint(dataObject(t, rec)["ID"].(float64))

	rec = request(t, tc.Router, http.MethodPost,
		"/api/v1/spaces/"+itoa(spaceID)+"/tags/merge",
		tc.Token,
		map[string]any{"source_tag_id": sourceID, "target_tag_id": targetID},
	)
	if rec.Code != http.StatusOK {
		t.Fatalf("merge tags: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var source domain.Tag
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
