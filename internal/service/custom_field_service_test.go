package service

import (
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
