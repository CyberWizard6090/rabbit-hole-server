package integration

import (
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	specPathLine   = regexp.MustCompile(`^  (/\S*):\s*$`)
	specMethodLine = regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	specStatusLine = regexp.MustCompile(`^        '?(\d{3})'?:`)
	ginParam       = regexp.MustCompile(`:([A-Za-z_]+)`)
)

type specOperation struct {
	statuses []string
}

func (o specOperation) documents(status string) bool {
	for _, s := range o.statuses {
		if s == status {
			return true
		}
	}
	return false
}

func loadSpecOperations(t *testing.T) map[string]specOperation {
	t.Helper()
	raw, err := os.ReadFile("../../docs/openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}
	ops := map[string]specOperation{}
	path, key := "", ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "components:") {
			break
		}
		if m := specPathLine.FindStringSubmatch(line); m != nil {
			path, key = m[1], ""
			continue
		}
		if m := specMethodLine.FindStringSubmatch(line); m != nil && path != "" {
			key = strings.ToUpper(m[1]) + " " + path
			ops[key] = specOperation{}
			continue
		}
		if m := specStatusLine.FindStringSubmatch(line); m != nil && key != "" {
			op := ops[key]
			op.statuses = append(op.statuses, m[1])
			ops[key] = op
		}
	}
	if len(ops) < 100 {
		t.Fatalf("parsed only %d operations from openapi.yaml", len(ops))
	}
	return ops
}

func TestOpenAPI_DocumentsExactlyTheRegisteredRoutes(t *testing.T) {
	tc := newTestContext(t)
	spec := loadSpecOperations(t)
	registered := map[string]bool{}
	for _, route := range apiRoutes(t, tc) {
		path := ginParam.ReplaceAllString(strings.TrimPrefix(route.Path, "/api/v1"), "{$1}")
		registered[route.Method+" "+path] = true
	}
	var missing, stale []string
	for key := range registered {
		if _, ok := spec[key]; !ok {
			missing = append(missing, key)
		}
	}
	for key := range spec {
		if !registered[key] {
			stale = append(stale, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 {
		t.Errorf("routes missing from docs/openapi.yaml:\n  %s", strings.Join(missing, "\n  "))
	}
	if len(stale) > 0 {
		t.Errorf("operations in docs/openapi.yaml without a route:\n  %s", strings.Join(stale, "\n  "))
	}
}

func TestOpenAPI_CreateEndpointsDocumentTheirRealSuccessStatus(t *testing.T) {
	tc := newTestContext(t)
	spec := loadSpecOperations(t)
	ownerID, token := newUser(t, tc, "openapi-status@example.test")
	fixture := buildRouteFixture(t, tc, token, ownerID)
	p := fixture.params
	ws := "/api/v1/workspaces/" + p["workspace_id"]

	cases := []struct {
		key  string
		path string
		body map[string]any
	}{
		{"POST /workspaces/{workspace_id}/spaces", ws + "/spaces", map[string]any{"name": "doc space"}},
		{"POST /spaces/{space_id}/folders", "/api/v1/spaces/" + p["space_id"] + "/folders", map[string]any{"name": "doc folder"}},
		{"POST /spaces/{space_id}/lists", "/api/v1/spaces/" + p["space_id"] + "/lists", map[string]any{"name": "doc list"}},
		{"POST /folders/{folder_id}/lists", "/api/v1/folders/" + p["folder_id"] + "/lists", map[string]any{"name": "doc folder list"}},
		{"POST /spaces/{space_id}/tags", "/api/v1/spaces/" + p["space_id"] + "/tags", map[string]any{"name": "doc tag", "color": "#112233"}},
		{"POST /lists/{list_id}/statuses", "/api/v1/lists/" + p["list_id"] + "/statuses", map[string]any{"name": "Doc", "color": "#445566", "type": 1, "list_id": mustAtoi(t, p["list_id"])}},
		{"POST /lists/{list_id}/tasks", "/api/v1/lists/" + p["list_id"] + "/tasks", map[string]any{"title": "doc task", "status_id": mustAtoi(t, p["status_id"])}},
		{"POST /tasks/{task_id}/checklists", "/api/v1/tasks/" + p["task_id"] + "/checklists", map[string]any{"name": "doc checklist"}},
		{"POST /tasks/{task_id}/checklists/{checklist_id}/items", "/api/v1/tasks/" + p["task_id"] + "/checklists/" + p["checklist_id"] + "/items", map[string]any{"text": "doc item"}},
		{"POST /tasks/{task_id}/comments", "/api/v1/tasks/" + p["task_id"] + "/comments", map[string]any{"body": "doc comment"}},
		{"POST /workspaces/{workspace_id}/custom-fields", ws + "/custom-fields", map[string]any{"name": "doc field", "type": "text"}},
		{"POST /workspaces", "/api/v1/workspaces", map[string]any{"name": "doc workspace"}},
	}
	for _, c := range cases {
		t.Run(c.key, func(t *testing.T) {
			rec := request(t, tc.Router, "POST", c.path, token, c.body)
			if rec.Code != 201 {
				t.Fatalf("real status = %d, want 201: %s", rec.Code, rec.Body.String())
			}
			op, ok := spec[c.key]
			if !ok {
				t.Fatalf("%s is not documented", c.key)
			}
			if !op.documents("201") || op.documents("200") {
				t.Fatalf("%s documents %v, want 201 and no 200", c.key, op.statuses)
			}
		})
	}
}

func mustAtoi(t *testing.T, raw string) int {
	t.Helper()
	v, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("not a number: %q", raw)
	}
	return v
}
