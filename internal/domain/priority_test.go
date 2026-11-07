package domain

import "testing"

func TestTaskPriorityScale(t *testing.T) {
	cases := map[string]int{"low": 1, "normal": 2, "high": 3, "urgent": 4, "critical": 5}
	for name, want := range cases {
		got, ok := TaskPriorityFromName(name)
		if !ok || got != want {
			t.Fatalf("TaskPriorityFromName(%q) = %d, %v; want %d", name, got, ok, want)
		}
		back, ok := TaskPriorityName(want)
		if !ok || back != name {
			t.Fatalf("TaskPriorityName(%d) = %q, %v; want %q", want, back, ok, name)
		}
		if !ValidTaskPriorityName(name) {
			t.Fatalf("ValidTaskPriorityName(%q) = false", name)
		}
	}
}

func TestTaskPriorityRejectsUnknownValues(t *testing.T) {
	for _, name := range []string{"", "LOW", "medium", "5", " high", "blocker"} {
		if got, ok := TaskPriorityFromName(name); ok || got != 0 {
			t.Fatalf("TaskPriorityFromName(%q) = %d, %v; want rejection", name, got, ok)
		}
		if ValidTaskPriorityName(name) {
			t.Fatalf("ValidTaskPriorityName(%q) = true", name)
		}
	}
	for _, value := range []int{-1, 0, 6, 100} {
		if name, ok := TaskPriorityName(value); ok || name != "" {
			t.Fatalf("TaskPriorityName(%d) = %q, %v; want rejection", value, name, ok)
		}
	}
}
