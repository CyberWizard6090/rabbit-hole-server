package integration

import (
	"net/http"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func createWorkspace(t *testing.T, tc *TestContext) uint {
	t.Helper()

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/workspaces/", tc.Token, map[string]any{
		"name": "Space Test Workspace",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create workspace: %d: %s", rec.Code, rec.Body.String())
	}

	return uint(dataObject(t, rec)["ID"].(float64))
}

func TestSpace_CreateCreatesDefaultListAndStatuses(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/workspaces/"+itoa(workspaceID)+"/spaces",
		tc.Token,
		map[string]any{"name": "Main Space", "currency": "EUR"},
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create space: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	space := dataObject(t, rec)
	spaceID := uint(space["ID"].(float64))

	var lists []domain.List
	if err := tc.DB.Where("space_id = ?", spaceID).Find(&lists).Error; err != nil {
		t.Fatalf("find default lists: %v", err)
	}
	if len(lists) != 1 {
		t.Fatalf("default lists count = %d, want 1", len(lists))
	}

	var statuses []domain.TaskStatus
	if err := tc.DB.Where("space_id = ?", spaceID).Order("position ASC").Find(&statuses).Error; err != nil {
		t.Fatalf("find default statuses: %v", err)
	}
	if len(statuses) != 3 {
		t.Fatalf("default statuses count = %d, want 3", len(statuses))
	}

	want := []struct {
		name string
		pos  int
		typ  domain.StatusType
	}{
		{"К выполнению", 1, domain.StatusTodo},
		{"В работе", 2, domain.StatusInProgress},
		{"Готово", 3, domain.StatusDone},
	}
	for i, w := range want {
		if statuses[i].Name != w.name || statuses[i].Position != w.pos || statuses[i].Type != w.typ {
			t.Fatalf("status[%d] = %+v, want name=%q position=%d type=%d", i, statuses[i], w.name, w.pos, w.typ)
		}
	}
}

func TestSpace_CreateRequiresWorkspacePermission(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	rec := request(t, tc.Router, http.MethodPost,
		"/api/v1/workspaces/"+itoa(workspaceID)+"/spaces",
		"",
		map[string]any{"name": "No Auth Space"},
	)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}

func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	buf := make([]byte, 0, 20)
	for v > 0 {
		buf = append([]byte{byte('0' + v%10)}, buf...)
		v /= 10
	}
	return string(buf)
}
