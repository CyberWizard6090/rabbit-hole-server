package apitypes

import (
	"os"
	"strings"
	"testing"
)

const (
	specPath  = "../../docs/openapi.yaml"
	typesPath = "../../docs/api-types.ts"
)

func TestGeneratedTypesMatchTheCommittedFile(t *testing.T) {
	want, err := Generate(specPath)
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("UPDATE_API_TYPES") != "" {
		if err := os.WriteFile(typesPath, []byte(want), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(typesPath)
	if err != nil {
		t.Fatalf("read %s: %v (run `make api-types`)", typesPath, err)
	}
	if string(got) != want {
		t.Fatalf("docs/api-types.ts is out of date with docs/openapi.yaml; run `make api-types` and commit the result")
	}
}

func TestEveryOperationHasAnEntryAndEveryReferenceResolves(t *testing.T) {
	source, err := Generate(specPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{
		`"GET /tasks/{task_id}": {`,
		`"POST /lists/{list_id}/tasks": {`,
		"export interface Task {",
		"export interface Pagination {",
	} {
		if !strings.Contains(source, must) {
			t.Errorf("generated source lacks %q", must)
		}
	}
}
