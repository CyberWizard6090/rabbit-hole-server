package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestTaskPriorityValueMapsEveryName(t *testing.T) {
	want := map[string]int{"low": 1, "normal": 2, "high": 3, "urgent": 4, "critical": 5, "": 2, "unknown": 2}
	for name, value := range want {
		if got := taskPriorityValue(name); got != value {
			t.Fatalf("taskPriorityValue(%q) = %d, want %d", name, got, value)
		}
	}
}

func TestNormalizeTaskTemplateConfigurationPriority(t *testing.T) {
	for _, name := range []string{"low", "normal", "high", "urgent", "critical"} {
		raw := json.RawMessage(`{"task":{"priority":"` + name + `"}}`)
		if _, err := NormalizeTaskTemplateConfiguration(raw); err != nil {
			t.Fatalf("priority %q rejected: %v", name, err)
		}
	}
	normalized, err := NormalizeTaskTemplateConfiguration(json.RawMessage(`{"task":{}}`))
	if err != nil || !strings.Contains(string(normalized), `"priority":"normal"`) {
		t.Fatalf("missing priority must default to normal: %s %v", normalized, err)
	}
	for _, name := range []string{"blocker", "Critical", "5"} {
		raw := json.RawMessage(`{"task":{"priority":"` + name + `"}}`)
		if _, err := NormalizeTaskTemplateConfiguration(raw); !errors.Is(err, domain.ErrInvalidTaskTemplate) {
			t.Fatalf("priority %q must be rejected, got %v", name, err)
		}
	}
}

func TestResolveSubmissionTaskPrecedence(t *testing.T) {
	statusDefault, statusTemplate := uint(10), uint(20)
	title, description := "Template title", "Template description"
	template := TaskTemplateConfiguration{}
	template.Task.Title, template.Task.Description = &title, &description
	template.Task.Priority, template.Task.StatusID = "high", &statusTemplate
	config := domain.FormTaskConfiguration{
		Target:       domain.FormTaskTarget{WorkspaceID: 1, ListID: 5},
		TaskDefaults: domain.FormTaskDefaults{Priority: "urgent", StatusID: &statusDefault},
		Mappings: []domain.FormTaskMapping{
			{QuestionID: 1, Target: domain.FormMappingTarget{Type: "task_field", Field: "title"}},
			{QuestionID: 2, Target: domain.FormMappingTarget{Type: "task_field", Field: "priority"},
				Transform: &domain.FormValueTransform{Type: "enum", Values: map[string]string{"x": "critical"}}},
		},
	}
	answer := func(questionID uint, value string) domain.SubmissionAnswer {
		return domain.SubmissionAnswer{QuestionID: questionID, Value: json.RawMessage(`"` + value + `"`)}
	}

	submission := domain.FormSubmission{ID: 7, Answers: []domain.SubmissionAnswer{answer(1, "Mapped title"), answer(2, "x")}}
	resolved, err := resolveSubmissionTask(submission, config, template, nil)
	input := resolved.input
	if err != nil {
		t.Fatal(err)
	}
	if input.Title != "Mapped title" || input.Priority != domain.TaskPriorityCritical ||
		input.StatusID != statusTemplate || input.Description != description {
		t.Fatalf("mapping must beat template and template must beat defaults: %+v", input)
	}

	submission.Answers = []domain.SubmissionAnswer{answer(1, "Mapped title")}
	resolved, err = resolveSubmissionTask(submission, config, template, nil)
	input = resolved.input
	if err != nil {
		t.Fatal(err)
	}
	if input.Priority != domain.TaskPriorityHigh || input.StatusID != statusTemplate {
		t.Fatalf("template must beat task_defaults when no answer is mapped: %+v", input)
	}

	resolved, err = resolveSubmissionTask(submission, config, TaskTemplateConfiguration{}, nil)
	input = resolved.input
	if err != nil {
		t.Fatal(err)
	}
	if input.Priority != domain.TaskPriorityUrgent || input.StatusID != statusDefault || input.Title != "Mapped title" {
		t.Fatalf("task_defaults without a template: %+v", input)
	}
}

func TestValidateQuestionSettings(t *testing.T) {
	cases := []struct {
		kind     domain.QuestionType
		settings string
		options  int
		field    string
	}{
		{domain.QuestionRating, `{}`, 0, ""},
		{domain.QuestionRating, `{"min":0,"max":10}`, 0, ""},
		{domain.QuestionRating, `{"max":11}`, 0, "settings.max"},
		{domain.QuestionRating, `{"min":3,"max":3}`, 0, "settings.min"},
		{domain.QuestionRating, `{"min":6}`, 0, "settings.min"},
		{domain.QuestionScale, `{"min":0,"max":100,"min_label":"a","max_label":"b"}`, 0, ""},
		{domain.QuestionScale, `{"max":101}`, 0, "settings.max"},
		{domain.QuestionNumber, `{"min":-1.5,"max":2.5}`, 0, ""},
		{domain.QuestionNumber, `{"min":3,"max":2}`, 0, "settings.min"},
		{domain.QuestionShortText, `{"max_length":1001}`, 0, "settings.max_length"},
		{domain.QuestionLongText, `{"max_length":1001}`, 0, ""},
		{domain.QuestionShortText, `{"color":1}`, 0, "settings.color"},
		{domain.QuestionCheckbox, `{"a":1}`, 0, "settings.a"},
		{domain.QuestionMultipleChoice, `{"max_selected":2}`, 2, ""},
		{domain.QuestionMultipleChoice, `{"max_selected":3}`, 2, "settings.max_selected"},
		{domain.QuestionFile, `{"anything":true}`, 0, ""},
		{domain.QuestionShortText, `[]`, 0, "settings"},
	}
	for _, tcase := range cases {
		err := validateQuestionSettings(tcase.kind, json.RawMessage(tcase.settings), tcase.options)
		var validation *domain.FormValidationError
		switch {
		case tcase.field == "" && err != nil:
			t.Fatalf("%s %s: unexpected error %v", tcase.kind, tcase.settings, err)
		case tcase.field != "" && (!errors.As(err, &validation) || validation.Field != tcase.field):
			t.Fatalf("%s %s: error %v, want field %s", tcase.kind, tcase.settings, err, tcase.field)
		}
	}
}

func TestValidateSubmissionRatingDefaultsAndUnknownQuestions(t *testing.T) {
	questions := []domain.FormQuestion{
		{ID: 1, Type: domain.QuestionRating, Settings: json.RawMessage(`{}`)},
		{ID: 2, Type: domain.QuestionScale, Settings: json.RawMessage(`{}`)},
		{ID: 3, Type: domain.QuestionShortText, Settings: json.RawMessage(`{"min_length":2,"max_length":3}`)},
	}
	check := func(input map[uint]json.RawMessage) error {
		_, err := validateSubmission(questions, input)
		return err
	}
	valid := map[uint]json.RawMessage{1: json.RawMessage(`5`), 2: json.RawMessage(`10`), 3: json.RawMessage(`"ab"`)}
	if err := check(valid); err != nil {
		t.Fatalf("valid answers rejected: %v", err)
	}
	for name, input := range map[string]map[uint]json.RawMessage{
		"rating 6":      {1: json.RawMessage(`6`)},
		"rating 0":      {1: json.RawMessage(`0`)},
		"rating 2.5":    {1: json.RawMessage(`2.5`)},
		"scale 11":      {2: json.RawMessage(`11`)},
		"text short":    {3: json.RawMessage(`"a"`)},
		"text long":     {3: json.RawMessage(`"abcd"`)},
		"unknown id":    {1: json.RawMessage(`3`), 99: json.RawMessage(`"x"`)},
		"unknown alone": {99: json.RawMessage(`"x"`)},
	} {
		if err := check(input); !errors.Is(err, domain.ErrInvalidSubmission) {
			t.Fatalf("%s: err = %v, want ErrInvalidSubmission", name, err)
		}
	}
}

func TestResolveQuestionIndexes(t *testing.T) {
	zero, one, five := 0, 1, 5
	created := []domain.FormQuestion{{ID: 100}, {ID: 200}}
	config := domain.FormTaskConfiguration{Mappings: []domain.FormTaskMapping{
		{QuestionIndex: &one}, {QuestionIndex: &zero},
	}}
	resolved, err := resolveQuestionIndexes(config, created)
	if err != nil || resolved.Mappings[0].QuestionID != 200 || resolved.Mappings[1].QuestionID != 100 ||
		resolved.Mappings[0].QuestionIndex != nil {
		t.Fatalf("resolved = %+v, %v", resolved.Mappings, err)
	}
	for name, mapping := range map[string]domain.FormTaskMapping{
		"out of range": {QuestionIndex: &five},
		"both":         {QuestionID: 3, QuestionIndex: &zero},
		"neither":      {},
		"id only":      {QuestionID: 3},
	} {
		_, err := resolveQuestionIndexes(domain.FormTaskConfiguration{Mappings: []domain.FormTaskMapping{mapping}}, created)
		var validation *domain.FormValidationError
		if !errors.As(err, &validation) || !strings.HasPrefix(validation.Field, "task_config.mappings[0]") {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestFormPresetsAreValidAndLocalized(t *testing.T) {
	english, russian := FormPresets(localeEnglish), FormPresets(localeRussian)
	if len(english) != 5 || len(russian) != len(english) {
		t.Fatalf("preset count: %d / %d", len(english), len(russian))
	}
	for i := range english {
		if english[i].Key != russian[i].Key || english[i].Name == russian[i].Name {
			t.Fatalf("preset %s is not localized", english[i].Key)
		}
		for _, preset := range []FormPreset{english[i], russian[i]} {
			if len(preset.Questions) == 0 || len(preset.Questions) > domain.MaxFormQuestions {
				t.Fatalf("preset %s has %d questions", preset.Key, len(preset.Questions))
			}
			inputs := make([]QuestionInput, len(preset.Questions))
			for j, question := range preset.Questions {
				options := make([]domain.QuestionOption, len(question.Options))
				for k, option := range question.Options {
					options[k] = domain.QuestionOption{Label: option.Label, Value: option.Value}
				}
				inputs[j] = QuestionInput{
					Type: question.Type, Title: question.Title, Description: question.Description,
					Required: question.Required, Settings: question.Settings, Options: options,
				}
			}
			if _, err := normalizeFormQuestions(inputs); err != nil {
				t.Fatalf("preset %s (%s) is not a valid create request: %v", preset.Key, preset.Name, err)
			}
		}
	}
}
