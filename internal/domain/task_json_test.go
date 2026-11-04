package domain

import (
	"encoding/json"
	"testing"
)

func TestTaskJSONAlwaysCarriesChecklistProgress(t *testing.T) {
	raw, err := json.Marshal(Task{})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"checklist_items_total", "checklist_items_resolved"} {
		if v, ok := got[key]; !ok || v != float64(0) {
			t.Fatalf("%s = %v (present %v), want 0", key, v, ok)
		}
	}
}
