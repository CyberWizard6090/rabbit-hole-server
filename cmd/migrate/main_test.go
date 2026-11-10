package main

import (
	"testing"

	"rabbit-hole-server/internal/database"
)

func TestResolveAction(t *testing.T) {
	cases := []struct {
		name        string
		action      string
		steps       int
		all         bool
		environment string
		want        string
		wantErr     bool
	}{
		{"up", "up", 0, false, "production", "up", false},
		{"down without steps", "down", 0, false, "development", "", true},
		{"down with steps", "down", 1, false, "production", "down", false},
		{"down all in development", "down", 0, true, "development", database.ActionDownAll, false},
		{"down all refused in production", "down", 0, true, "production", "", true},
		{"steps and all together", "down", 2, true, "development", "", true},
		{"all with another action", "up", 0, true, "development", "", true},
		{"version", "version", 0, false, "production", "version", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveAction(tc.action, tc.steps, tc.all, tc.environment)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("resolveAction = %q, %v; want %q, error=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
