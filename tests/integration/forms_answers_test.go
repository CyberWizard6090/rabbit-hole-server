package integration

import (
	"fmt"
	"net/http"
	"testing"
)

func TestFormQuestions_SettingsAreValidatedPerType(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcsettings")
	_, formURL := env.newForm(t, tc, "Settings")
	options := []any{map[string]any{"label": "A", "value": "a"}, map[string]any{"label": "B", "value": "b"}}

	cases := []struct {
		name     string
		question map[string]any
		field    string
	}{
		{"rating max above 10", map[string]any{"type": "rating", "title": "r", "settings": map[string]any{"max": 11}}, "settings.max"},
		{"rating min not below max", map[string]any{"type": "rating", "title": "r", "settings": map[string]any{"min": 5, "max": 5}}, "settings.min"},
		{"rating default max with high min", map[string]any{"type": "rating", "title": "r", "settings": map[string]any{"min": 6}}, "settings.min"},
		{"rating fractional", map[string]any{"type": "rating", "title": "r", "settings": map[string]any{"max": 4.5}}, "settings.max"},
		{"scale max above 100", map[string]any{"type": "scale", "title": "s", "settings": map[string]any{"max": 101}}, "settings.max"},
		{"scale label too long", map[string]any{"type": "scale", "title": "s", "settings": map[string]any{"min_label": string(make([]byte, 101))}}, ""},
		{"number min above max", map[string]any{"type": "number", "title": "n", "settings": map[string]any{"min": 10, "max": 1}}, "settings.min"},
		{"number bound is a string", map[string]any{"type": "number", "title": "n", "settings": map[string]any{"min": "1"}}, "settings.min"},
		{"text max_length zero", map[string]any{"type": "short_text", "title": "t", "settings": map[string]any{"max_length": 0}}, "settings.max_length"},
		{"text min above max", map[string]any{"type": "long_text", "title": "t", "settings": map[string]any{"min_length": 9, "max_length": 3}}, "settings.min_length"},
		{"placeholder not a string", map[string]any{"type": "email", "title": "e", "settings": map[string]any{"placeholder": 7}}, "settings.placeholder"},
		{"unknown key", map[string]any{"type": "checkbox", "title": "c", "settings": map[string]any{"color": "red"}}, "settings.color"},
		{"static question with settings", map[string]any{"type": "divider", "title": "d", "settings": map[string]any{"min": 1}}, "settings.min"},
		{"multiple choice max above options", map[string]any{"type": "multiple_choice", "title": "m", "options": options,
			"settings": map[string]any{"max_selected": 3}}, "settings.max_selected"},
		{"multiple choice min above max", map[string]any{"type": "multiple_choice", "title": "m", "options": options,
			"settings": map[string]any{"min_selected": 2, "max_selected": 1}}, "settings.min_selected"},
		{"settings is an array", map[string]any{"type": "short_text", "title": "t", "settings": []any{1}}, "settings"},
		{"settings is null", map[string]any{"type": "short_text", "title": "t", "settings": nil}, ""},
	}
	for _, tcase := range cases {
		t.Run(tcase.name, func(t *testing.T) {
			rec := request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token, tcase.question)
			if tcase.name == "settings is null" {
				if rec.Code != http.StatusCreated {
					t.Fatalf("null settings mean no settings: %d: %s", rec.Code, rec.Body.String())
				}
				return
			}
			if tcase.field == "" {
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
				}
				return
			}
			fcExpectError(t, rec, http.StatusBadRequest, "INVALID_FORM", tcase.field)
		})
	}

	valid := []map[string]any{
		{"type": "rating", "title": "r", "settings": map[string]any{"min": 0, "max": 10}},
		{"type": "scale", "title": "s", "settings": map[string]any{"min": 0, "max": 100, "min_label": "Never", "max_label": "Always"}},
		{"type": "number", "title": "n", "settings": map[string]any{"min": -5.5, "max": 1000, "placeholder": "0"}},
		{"type": "short_text", "title": "t", "settings": map[string]any{"min_length": 2, "max_length": 20, "placeholder": "Name"}},
		{"type": "multiple_choice", "title": "m", "options": options, "settings": map[string]any{"min_selected": 1, "max_selected": 2}},
		{"type": "dropdown", "title": "d", "options": options, "settings": map[string]any{"placeholder": "Pick"}},
		{"type": "file", "title": "f", "settings": map[string]any{"anything": true}},
	}
	for _, question := range valid {
		rec := request(t, tc.Router, http.MethodPost, formURL+"/questions", env.owner.token, question)
		if rec.Code != http.StatusCreated {
			t.Fatalf("valid %v settings rejected: %d: %s", question["type"], rec.Code, rec.Body.String())
		}
	}
}

func TestFormSubmissions_AnswerValuesAreValidatedPerQuestionType(t *testing.T) {
	tc := newTestContext(t)
	env := newFCEnv(t, tc, "fcanswers")
	_, formURL := env.newForm(t, tc, "Answers")
	choices := []any{
		map[string]any{"label": "Red", "value": "red"},
		map[string]any{"label": "Green", "value": "green"},
		map[string]any{"label": "Blue", "value": "blue"},
	}
	add := func(question map[string]any) string {
		t.Helper()
		return itoa(mustCreate(t, tc, env.owner.token, formURL+"/questions", question))
	}
	ids := map[string]string{
		"short":    add(map[string]any{"type": "short_text", "title": "Short", "settings": map[string]any{"min_length": 2, "max_length": 5}}),
		"long":     add(map[string]any{"type": "long_text", "title": "Long"}),
		"number":   add(map[string]any{"type": "number", "title": "Number", "settings": map[string]any{"min": 0, "max": 10}}),
		"email":    add(map[string]any{"type": "email", "title": "Email"}),
		"phone":    add(map[string]any{"type": "phone", "title": "Phone"}),
		"url":      add(map[string]any{"type": "url", "title": "Url"}),
		"date":     add(map[string]any{"type": "date", "title": "Date"}),
		"datetime": add(map[string]any{"type": "datetime", "title": "Datetime"}),
		"single":   add(map[string]any{"type": "single_choice", "title": "Single", "options": choices}),
		"dropdown": add(map[string]any{"type": "dropdown", "title": "Dropdown", "options": choices}),
		"multiple": add(map[string]any{"type": "multiple_choice", "title": "Multiple", "options": choices,
			"settings": map[string]any{"min_selected": 1, "max_selected": 2}}),
		"checkbox": add(map[string]any{"type": "checkbox", "title": "Checkbox"}),
		"rating":   add(map[string]any{"type": "rating", "title": "Rating"}),
		"scale":    add(map[string]any{"type": "scale", "title": "Scale"}),
		"custom":   add(map[string]any{"type": "rating", "title": "Custom rating", "settings": map[string]any{"min": 0, "max": 3}}),
		"section":  add(map[string]any{"type": "section", "title": "Section"}),
	}
	rec := request(t, tc.Router, http.MethodPost, formURL+"/publish", env.owner.token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("publish: %d: %s", rec.Code, rec.Body.String())
	}
	slug := dataObject(t, request(t, tc.Router, http.MethodGet, formURL, env.owner.token, nil))["slug"].(string)

	good := map[string]any{
		"short": "abc", "long": "Some long text", "number": 7.5, "email": "person@example.com", "phone": "+1 555 0100",
		"url": "https://example.com/path", "date": "2026-02-28", "datetime": "2026-02-28T10:00:00+03:00",
		"single": "red", "dropdown": "green", "multiple": []any{"red", "blue"}, "checkbox": false,
		"rating": 5, "scale": 10, "custom": 0,
	}
	answers := func(overrides map[string]any) map[string]any {
		result := map[string]any{}
		for key, value := range good {
			result[ids[key]] = value
		}
		for key, value := range overrides {
			if value == nil {
				delete(result, ids[key])
				continue
			}
			result[ids[key]] = value
		}
		return result
	}

	if rec := fcSubmit(t, tc, slug, "good", answers(nil)); rec.Code != http.StatusCreated {
		t.Fatalf("a valid answer set must be accepted: %d: %s", rec.Code, rec.Body.String())
	}
	if rec := fcSubmit(t, tc, slug, "only-checkbox", map[string]any{ids["checkbox"]: true}); rec.Code != http.StatusCreated {
		t.Fatalf("every question is optional: %d: %s", rec.Code, rec.Body.String())
	}

	bad := map[string]map[string]any{
		"short too short":    {"short": "a"},
		"short too long":     {"short": "abcdef"},
		"short is number":    {"short": 5},
		"number below min":   {"number": -1},
		"number above max":   {"number": 10.5},
		"number is string":   {"number": "5"},
		"email malformed":    {"email": "not-an-email"},
		"email with name":    {"email": "Person <person@example.com>"},
		"phone is number":    {"phone": 5551234},
		"url without scheme": {"url": "example.com"},
		"url ftp":            {"url": "ftp://example.com"},
		"date wrong format":  {"date": "28.02.2026"},
		"date impossible":    {"date": "2026-02-30"},
		"datetime no zone":   {"datetime": "2026-02-28T10:00:00"},
		"single unknown":     {"single": "purple"},
		"single is array":    {"single": []any{"red"}},
		"dropdown unknown":   {"dropdown": "purple"},
		"multiple is string": {"multiple": "red"},
		"multiple duplicate": {"multiple": []any{"red", "red"}},
		"multiple too many":  {"multiple": []any{"red", "green", "blue"}},
		"multiple unknown":   {"multiple": []any{"red", "purple"}},
		"checkbox string":    {"checkbox": "true"},
		"rating above 5":     {"rating": 6},
		"rating below 1":     {"rating": 0},
		"rating fractional":  {"rating": 2.5},
		"scale above 10":     {"scale": 11},
		"scale below 1":      {"scale": 0},
		"custom above max":   {"custom": 4},
		"custom below min":   {"custom": -1},
		"empty string":       {"long": "   "},
		"empty array":        {"multiple": []any{}},
		"null value":         {"phone": nil},
		"section answered":   {"section": "x"},
	}
	n := 0
	for name, overrides := range bad {
		n++
		body := answers(overrides)
		if value, ok := overrides["phone"]; ok && value == nil {
			body[ids["phone"]] = nil
		}
		rec := fcSubmit(t, tc, slug, fmt.Sprintf("bad-%d", n), body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d, want 400: %s", name, rec.Code, rec.Body.String())
		}
	}

	rec = fcSubmit(t, tc, slug, "unknown-question", map[string]any{ids["short"]: "abc", "999999": "ghost"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("an answer to an unknown question must be rejected: %d: %s", rec.Code, rec.Body.String())
	}
}
