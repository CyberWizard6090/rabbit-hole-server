package integration

import (
	"net/http"
	"testing"
)

func TestCreateSpace_IsAtomic(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	for _, stmt := range []string{
		`CREATE OR REPLACE FUNCTION test_fail_status_insert() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'simulated failure'; END $$ LANGUAGE plpgsql`,
		`CREATE TRIGGER test_fail_status_insert BEFORE INSERT ON task_statuses FOR EACH ROW EXECUTE FUNCTION test_fail_status_insert()`,
	} {
		if err := tc.DB.Exec(stmt).Error; err != nil {
			t.Fatalf("install failure trigger: %v", err)
		}
	}
	t.Cleanup(func() {
		tc.DB.Exec(`DROP TRIGGER IF EXISTS test_fail_status_insert ON task_statuses`)
		tc.DB.Exec(`DROP FUNCTION IF EXISTS test_fail_status_insert()`)
	})

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", tc.Token, map[string]any{"name": "Doomed"})
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 from the simulated failure, got %d: %s", rec.Code, rec.Body.String())
	}

	var spaces, lists int64
	tc.DB.Table("spaces").Where("workspace_id = ?", workspaceID).Count(&spaces)
	tc.DB.Table("lists").Count(&lists)
	if spaces != 0 || lists != 0 {
		t.Fatalf("partial data left behind after a failed create: %d spaces, %d lists", spaces, lists)
	}
}

func TestCreateSpace_Succeeds_WithDefaultListAndStatuses(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", tc.Token, map[string]any{"name": "Fine"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: %d: %s", rec.Code, rec.Body.String())
	}
	spaceID := responseID(t, rec)

	var lists, statuses int64
	tc.DB.Table("lists").Where("space_id = ?", spaceID).Count(&lists)
	tc.DB.Table("task_statuses").Where("space_id = ?", spaceID).Count(&statuses)
	if lists != 1 || statuses != 3 {
		t.Fatalf("defaults: %d lists, %d statuses; want 1 and 3", lists, statuses)
	}
}
