package integration

import (
	"net/http"
	"testing"
)

func TestSpaceList_PagesAreStableWhenTimestampsTie(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	for i := 0; i < 5; i++ {
		rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", tc.Token, map[string]any{"name": "tie"})
		if rec.Code != http.StatusCreated {
			t.Fatalf("create space: %d: %s", rec.Code, rec.Body.String())
		}
	}
	if err := tc.DB.Exec("UPDATE spaces SET created_at = '2026-01-01T00:00:00Z' WHERE workspace_id = ?", workspaceID).Error; err != nil {
		t.Fatal(err)
	}

	var ids []uint
	for page := 1; page <= 3; page++ {
		rec := request(t, tc.Router, http.MethodGet, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces?limit=2&page="+itoa(uint(page)), tc.Token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("page %d: %d: %s", page, rec.Code, rec.Body.String())
		}
		for _, item := range decodeJSON(t, rec)["data"].([]any) {
			m := item.(map[string]any)
			v, ok := m["id"]
			if !ok {
				v = m["ID"]
			}
			ids = append(ids, uint(v.(float64)))
		}
	}

	if len(ids) != 5 {
		t.Fatalf("collected %d spaces across pages, want 5: %v", len(ids), ids)
	}
	for i := 1; i < len(ids); i++ {
		if ids[i] >= ids[i-1] {
			t.Fatalf("ids not strictly descending (duplicate or skipped row across pages): %v", ids)
		}
	}
}
