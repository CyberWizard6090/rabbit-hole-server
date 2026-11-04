package integration

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sort"
	"testing"
)

var snakeCaseKey = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

func collectBadKeys(path string, v any, bad *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if !snakeCaseKey.MatchString(k) || k == "deleted_at" {
				*bad = append(*bad, path+"."+k)
			}
			collectBadKeys(path+"."+k, child, bad)
		}
	case []any:
		for _, child := range t {
			collectBadKeys(path+"[]", child, bad)
		}
	}
}

func TestJSONContract_AllResponsesUseSnakeCaseKeys(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	workspaceID := createWorkspace(t, tc)

	post := func(path string, body map[string]any) uint {
		t.Helper()
		rec := request(t, tc.Router, http.MethodPost, path, tc.Token, body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST %s: %d: %s", path, rec.Code, rec.Body.String())
		}
		checkKeys(t, "POST "+path, rec.Body.Bytes())
		return responseID(t, rec)
	}
	spaceID := post("/api/v1/workspaces/"+itoa(workspaceID)+"/spaces", map[string]any{"name": "S"})
	folderID := post("/api/v1/spaces/"+itoa(spaceID)+"/folders", map[string]any{"name": "F"})
	listID := post("/api/v1/spaces/"+itoa(spaceID)+"/lists", map[string]any{"name": "L"})
	post("/api/v1/folders/"+itoa(folderID)+"/lists", map[string]any{"name": "L2"})
	statusID := post("/api/v1/lists/"+itoa(listID)+"/statuses", map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	post("/api/v1/spaces/"+itoa(spaceID)+"/tags", map[string]any{"name": "tag", "color": "#445566"})
	post("/api/v1/lists/"+itoa(listID)+"/tasks", map[string]any{"title": "a task", "status_id": statusID, "new_tag_names": []string{"x"}})

	for _, path := range []string{
		"/api/v1/workspaces",
		"/api/v1/workspaces/" + itoa(workspaceID) + "/spaces",
		"/api/v1/spaces/" + itoa(spaceID) + "/dashboard",
		"/api/v1/spaces/" + itoa(spaceID) + "/folders",
		"/api/v1/spaces/" + itoa(spaceID) + "/lists",
		"/api/v1/spaces/" + itoa(spaceID) + "/tags",
		"/api/v1/lists/" + itoa(listID) + "/statuses",
		"/api/v1/lists/" + itoa(listID) + "/tasks",
		"/api/v1/profile",
		"/api/v1/contacts",
	} {
		rec := request(t, tc.Router, http.MethodGet, path, tc.Token, nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: %d: %s", path, rec.Code, rec.Body.String())
		}
		checkKeys(t, "GET "+path, rec.Body.Bytes())
	}
}

func checkKeys(t *testing.T, label string, body []byte) {
	t.Helper()
	decoded := decodeBytes(t, body)
	var bad []string
	collectBadKeys("$", decoded, &bad)
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Errorf("%s has non-snake_case or internal keys: %v", label, bad)
	}
}

func decodeBytes(t *testing.T, body []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatalf("response is not JSON: %v: %s", err, body)
	}
	return v
}
