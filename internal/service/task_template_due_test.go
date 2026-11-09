package service

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"rabbit-hole-server/internal/domain"
)

func dueConfig(t *testing.T, task string) TaskTemplateConfiguration {
	t.Helper()
	var config TaskTemplateConfiguration
	if err := json.Unmarshal([]byte(`{"task":`+task+`}`), &config); err != nil {
		t.Fatalf("decode %s: %v", task, err)
	}
	return config
}

func TestTemplateDueDate(t *testing.T) {
	reference := time.Date(2026, 11, 8, 23, 59, 0, 0, time.FixedZone("far", 5*3600))
	cases := []struct {
		name string
		task string
		want string
		err  bool
	}{
		{name: "none", task: `{}`},
		{name: "absolute", task: `{"due_date":"2031-02-03"}`, want: "2031-02-03"},
		{name: "same day", task: `{"due_in_days":0}`, want: "2026-11-08"},
		{name: "relative", task: `{"due_in_days":30}`, want: "2026-12-08"},
		{name: "maximum", task: `{"due_in_days":3650}`, want: "2036-11-05"},
		{name: "too far", task: `{"due_in_days":3651}`, err: true},
		{name: "negative", task: `{"due_in_days":-1}`, err: true},
		{name: "bad layout", task: `{"due_date":"03.02.2031"}`, err: true},
		{name: "both", task: `{"due_date":"2031-02-03","due_in_days":1}`, err: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := templateDueDate(dueConfig(t, tc.task), reference)
			if tc.err {
				if !errors.Is(err, domain.ErrInvalidTaskTemplate) {
					t.Fatalf("err = %v, want ErrInvalidTaskTemplate", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == "" {
				if got != nil {
					t.Fatalf("got %v, want no due date", got)
				}
				return
			}
			if got == nil || got.Format("2006-01-02") != tc.want || got.Location() != time.UTC {
				t.Fatalf("got %v, want %s UTC", got, tc.want)
			}
		})
	}
}

func TestNormalizeTaskTemplateConfigurationValidatesDueDate(t *testing.T) {
	if _, err := NormalizeTaskTemplateConfiguration(json.RawMessage(`{"task":{"due_in_days":2}}`)); err != nil {
		t.Fatalf("relative due date rejected: %v", err)
	}
	for _, raw := range []string{
		`{"task":{"due_date":"tomorrow"}}`,
		`{"task":{"due_in_days":1.5}}`,
		`{"task":{"due_date":"2031-02-03","due_in_days":1}}`,
	} {
		if _, err := NormalizeTaskTemplateConfiguration(json.RawMessage(raw)); !errors.Is(err, domain.ErrInvalidTaskTemplate) {
			t.Errorf("%s: err = %v, want ErrInvalidTaskTemplate", raw, err)
		}
	}
}
