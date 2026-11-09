package service

import (
	"errors"
	"strings"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func TestCustomFieldFromInputValidatesOptions(t *testing.T) {
	tests := []struct {
		name    string
		input   CustomFieldInput
		wantErr bool
	}{
		{
			name: "dropdown options",
			input: CustomFieldInput{
				Name: "Priority", Type: domain.CustomFieldDropdown,
				Options: []domain.CustomFieldOption{{Label: "High", Value: "high"}},
			},
		},
		{
			name: "duplicate option",
			input: CustomFieldInput{
				Name: "Priority", Type: domain.CustomFieldDropdown,
				Options: []domain.CustomFieldOption{
					{Label: "High", Value: "high"}, {Label: "Urgent", Value: "high"},
				},
			},
			wantErr: true,
		},
		{
			name:    "missing dropdown options",
			input:   CustomFieldInput{Name: "Priority", Type: domain.CustomFieldDropdown},
			wantErr: true,
		},
		{
			name: "options on text field",
			input: CustomFieldInput{
				Name: "Summary", Type: domain.CustomFieldText,
				Options: []domain.CustomFieldOption{{Label: "A", Value: "a"}},
			},
			wantErr: true,
		},
		{
			name:    "unknown type",
			input:   CustomFieldInput{Name: "Anything", Type: "object"},
			wantErr: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := customFieldFromInput(10, 0, test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("error=%v, wantErr=%t", err, test.wantErr)
			}
		})
	}
}

func TestValidateCustomFieldValue(t *testing.T) {
	field := domain.CustomField{
		Name: "Priority", Type: domain.CustomFieldDropdown,
		Options: []domain.CustomFieldOption{{Label: "High", Value: "high"}},
	}
	if err := validateCustomFieldValue(field, "high"); err != nil {
		t.Fatalf("valid option rejected: %v", err)
	}
	if err := validateCustomFieldValue(field, "unknown"); err == nil {
		t.Fatal("invalid option accepted")
	}

	numberField := domain.CustomField{
		Name: "Score", Type: domain.CustomFieldInteger,
		Config: map[string]any{"min": float64(1), "max": float64(10)},
	}
	for _, value := range []any{float64(1), float64(10), int(5)} {
		if err := validateCustomFieldValue(numberField, value); err != nil {
			t.Errorf("valid integer %v rejected: %v", value, err)
		}
	}
	for _, value := range []any{float64(0), float64(11), float64(1.5), "5"} {
		if err := validateCustomFieldValue(numberField, value); err == nil {
			t.Errorf("invalid integer %v accepted", value)
		}
	}
}

func customFieldProblem(t *testing.T, err error) domain.FieldProblem {
	t.Helper()
	var fieldErr *domain.FieldError
	if !errors.As(err, &fieldErr) || !errors.Is(err, domain.ErrInvalidCustomField) || len(fieldErr.Problems) != 1 {
		t.Fatalf("err = %v, want a single field problem", err)
	}
	return fieldErr.Problems[0]
}

func TestCustomFieldInputErrorsNameThePath(t *testing.T) {
	cases := map[string]struct {
		input  CustomFieldInput
		field  string
		reason string
	}{
		"missing name":   {CustomFieldInput{Name: " ", Type: domain.CustomFieldText}, "name", "REQUIRED"},
		"long name":      {CustomFieldInput{Name: strings.Repeat("n", 256), Type: domain.CustomFieldText}, "name", "TOO_LONG"},
		"unknown type":   {CustomFieldInput{Name: "x", Type: "object"}, "type", "INVALID_CHOICE"},
		"bad config":     {CustomFieldInput{Name: "x", Type: domain.CustomFieldNumber, Config: map[string]any{"min": 5.0, "max": 1.0}}, "config", "INVALID_VALUE"},
		"no options":     {CustomFieldInput{Name: "x", Type: domain.CustomFieldDropdown}, "options", "INVALID_COUNT"},
		"stray options":  {CustomFieldInput{Name: "x", Type: domain.CustomFieldText, Options: []domain.CustomFieldOption{{Label: "a", Value: "a"}}}, "options", "NOT_ALLOWED"},
		"blank option":   {CustomFieldInput{Name: "x", Type: domain.CustomFieldDropdown, Options: []domain.CustomFieldOption{{Label: "a", Value: "a"}, {Label: " ", Value: "b"}}}, "options[1]", "INVALID_OPTION"},
		"repeated value": {CustomFieldInput{Name: "x", Type: domain.CustomFieldDropdown, Options: []domain.CustomFieldOption{{Label: "a", Value: "a"}, {Label: "b", Value: "a"}}}, "options[1].value", "DUPLICATE"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := customFieldFromInput(10, 0, test.input)
			problem := customFieldProblem(t, err)
			if problem.Field != test.field || problem.Reason != test.reason {
				t.Fatalf("problem = %+v, want %s %s", problem, test.field, test.reason)
			}
		})
	}
}

func TestCustomFieldValueErrorsCarryReasonAndParams(t *testing.T) {
	text := domain.CustomField{Type: domain.CustomFieldText, Config: map[string]any{"min_length": float64(2), "max_length": float64(3)}}
	number := domain.CustomField{Type: domain.CustomFieldNumber, Config: map[string]any{"min": float64(1), "max": float64(5)}}
	dropdown := domain.CustomField{Type: domain.CustomFieldDropdown, Options: []domain.CustomFieldOption{{Label: "A", Value: "a"}}}
	cases := []struct {
		name   string
		field  domain.CustomField
		value  any
		reason string
		param  string
	}{
		{"wrong type", text, 5, "INVALID_TYPE", "expected"},
		{"too short", text, "a", "TOO_SHORT", "min"},
		{"too long", text, "abcd", "TOO_LONG", "max"},
		{"out of range", number, 9.0, "OUT_OF_RANGE", "max"},
		{"not a number", number, "x", "INVALID_TYPE", "expected"},
		{"unknown option", dropdown, "z", "INVALID_CHOICE", "allowed"},
		{"bad email", domain.CustomField{Type: domain.CustomFieldEmail}, "nope", "INVALID_FORMAT", "format"},
		{"bad date", domain.CustomField{Type: domain.CustomFieldDate}, "2020-13-01", "INVALID_FORMAT", "format"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			problem := customFieldProblem(t, validateCustomFieldValue(test.field, test.value))
			if problem.Field != "value" || problem.Reason != test.reason {
				t.Fatalf("problem = %+v", problem)
			}
			if _, ok := problem.Params[test.param]; !ok {
				t.Fatalf("params = %v lack %q", problem.Params, test.param)
			}
		})
	}
}
