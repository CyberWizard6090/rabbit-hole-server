package service

import (
	"encoding/json"
	"errors"
	"testing"

	"rabbit-hole-server/internal/domain"
)

func visibleIf(questionID uint, operator string, value any) json.RawMessage {
	rule := map[string]any{"question_id": questionID, "operator": operator}
	if value != nil {
		rule["value"] = value
	}
	raw, _ := json.Marshal(map[string]any{"visible_if": rule})
	return raw
}

func choiceQuestion(id uint, position int, values ...string) domain.FormQuestion {
	options := make([]domain.QuestionOption, len(values))
	for i, value := range values {
		options[i] = domain.QuestionOption{Label: value, Value: value, Position: i}
	}
	return domain.FormQuestion{
		ID: id, Type: domain.QuestionSingleChoice, Title: "choice", Position: position, Options: options,
	}
}

func textQuestion(id uint, position int, required bool, settings json.RawMessage) domain.FormQuestion {
	return domain.FormQuestion{
		ID: id, Type: domain.QuestionShortText, Title: "text", Position: position, Required: required,
		Settings: settings,
	}
}

func formErrorCode(err error) string {
	var formErr *domain.FormError
	if errors.As(err, &formErr) {
		return formErr.Code
	}
	return ""
}

func answersInput(values map[uint]any) map[uint]json.RawMessage {
	out := make(map[uint]json.RawMessage, len(values))
	for id, value := range values {
		raw, _ := json.Marshal(value)
		out[id] = raw
	}
	return out
}

func answeredIDs(answers []domain.SubmissionAnswer) map[uint]bool {
	out := map[uint]bool{}
	for _, answer := range answers {
		out[answer.QuestionID] = true
	}
	return out
}

func TestParseVisibility(t *testing.T) {
	cases := []struct {
		name     string
		settings json.RawMessage
		wantNil  bool
		wantErr  bool
	}{
		{"empty settings", nil, true, false},
		{"no rule", json.RawMessage(`{"max":3}`), true, false},
		{"null rule", json.RawMessage(`{"visible_if":null}`), true, false},
		{"valid equals", visibleIf(1, "equals", "a"), false, false},
		{"valid any without value", visibleIf(1, "any", nil), false, false},
		{"unknown operator", visibleIf(1, "starts_with", "a"), false, true},
		{"missing value", visibleIf(1, "equals", nil), false, true},
		{"object value", visibleIf(1, "equals", map[string]any{"a": 1}), false, true},
		{"missing question", json.RawMessage(`{"visible_if":{"operator":"any"}}`), false, true},
		{"unknown field", json.RawMessage(`{"visible_if":{"question_id":1,"operator":"any","extra":1}}`), false, true},
		{"not an object", json.RawMessage(`{"visible_if":"x"}`), false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rule, err := parseVisibility(tc.settings)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil && (rule == nil) != tc.wantNil {
				t.Fatalf("rule = %v, wantNil %v", rule, tc.wantNil)
			}
			if err != nil && formErrorCode(err) != visibilityInvalidCode {
				t.Fatalf("code = %q", formErrorCode(err))
			}
		})
	}
}

func TestVisibilityErrors(t *testing.T) {
	base := choiceQuestion(1, 0, "yes", "no")
	t.Run("valid reference to an earlier question", func(t *testing.T) {
		errs := visibilityErrors([]domain.FormQuestion{base, textQuestion(2, 1, false, visibleIf(1, "equals", "yes"))})
		if len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})
	t.Run("self reference is a cycle", func(t *testing.T) {
		errs := visibilityErrors([]domain.FormQuestion{base, textQuestion(2, 1, false, visibleIf(2, "any", nil))})
		if formErrorCode(errs[2]) != visibilityCycleCode {
			t.Fatalf("errs = %v", errs)
		}
	})
	t.Run("two questions depending on each other form a cycle", func(t *testing.T) {
		errs := visibilityErrors([]domain.FormQuestion{
			textQuestion(1, 0, false, visibleIf(2, "any", nil)),
			textQuestion(2, 1, false, visibleIf(1, "any", nil)),
		})
		if formErrorCode(errs[1]) != visibilityCycleCode || formErrorCode(errs[2]) != visibilityCycleCode {
			t.Fatalf("errs = %v", errs)
		}
	})
	t.Run("longer cycle", func(t *testing.T) {
		errs := visibilityErrors([]domain.FormQuestion{
			textQuestion(1, 0, false, visibleIf(3, "any", nil)),
			textQuestion(2, 1, false, visibleIf(1, "any", nil)),
			textQuestion(3, 2, false, visibleIf(2, "any", nil)),
		})
		for _, id := range []uint{1, 2, 3} {
			if formErrorCode(errs[id]) != visibilityCycleCode {
				t.Fatalf("question %d: errs = %v", id, errs)
			}
		}
	})
	t.Run("unknown question", func(t *testing.T) {
		errs := visibilityErrors([]domain.FormQuestion{base, textQuestion(2, 1, false, visibleIf(99, "any", nil))})
		if formErrorCode(errs[2]) != visibilityInvalidCode {
			t.Fatalf("errs = %v", errs)
		}
	})
	t.Run("reference to a later question", func(t *testing.T) {
		errs := visibilityErrors([]domain.FormQuestion{textQuestion(1, 0, false, visibleIf(2, "any", nil)), choiceQuestion(2, 1, "a")})
		if formErrorCode(errs[1]) != visibilityInvalidCode {
			t.Fatalf("errs = %v", errs)
		}
	})
	t.Run("same position falls back to the identifier order", func(t *testing.T) {
		errs := visibilityErrors([]domain.FormQuestion{base, textQuestion(2, 0, false, visibleIf(1, "any", nil))})
		if len(errs) != 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})
	t.Run("static question cannot be a source", func(t *testing.T) {
		section := domain.FormQuestion{ID: 1, Type: domain.QuestionSection, Title: "s", Position: 0}
		errs := visibilityErrors([]domain.FormQuestion{section, textQuestion(2, 1, false, visibleIf(1, "any", nil))})
		if formErrorCode(errs[2]) != visibilityInvalidCode {
			t.Fatalf("errs = %v", errs)
		}
	})
	t.Run("choice value must be an option", func(t *testing.T) {
		errs := visibilityErrors([]domain.FormQuestion{base, textQuestion(2, 1, false, visibleIf(1, "equals", "maybe"))})
		if formErrorCode(errs[2]) != visibilityInvalidCode {
			t.Fatalf("errs = %v", errs)
		}
	})
	t.Run("checkbox needs a boolean", func(t *testing.T) {
		checkbox := domain.FormQuestion{ID: 1, Type: domain.QuestionCheckbox, Title: "c", Position: 0}
		if errs := visibilityErrors([]domain.FormQuestion{checkbox, textQuestion(2, 1, false, visibleIf(1, "equals", true))}); len(errs) != 0 {
			t.Fatalf("errs = %v", errs)
		}
		if errs := visibilityErrors([]domain.FormQuestion{checkbox, textQuestion(2, 1, false, visibleIf(1, "equals", "true"))}); errs[2] == nil {
			t.Fatal("string value for a checkbox must be rejected")
		}
	})
	t.Run("number needs a number", func(t *testing.T) {
		number := domain.FormQuestion{ID: 1, Type: domain.QuestionRating, Title: "r", Position: 0}
		if errs := visibilityErrors([]domain.FormQuestion{number, textQuestion(2, 1, false, visibleIf(1, "equals", 5))}); len(errs) != 0 {
			t.Fatalf("errs = %v", errs)
		}
		if errs := visibilityErrors([]domain.FormQuestion{number, textQuestion(2, 1, false, visibleIf(1, "contains", 5))}); errs[2] == nil {
			t.Fatal("contains on a number must be rejected")
		}
	})
	t.Run("new question sorts after existing ones at the same position", func(t *testing.T) {
		existing := []domain.FormQuestion{base}
		newQuestion := textQuestion(0, 0, false, visibleIf(1, "any", nil))
		if err := savedQuestionVisibilityError(existing, newQuestion); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSavedQuestionVisibilityError(t *testing.T) {
	first := choiceQuestion(1, 0, "a", "b")
	second := textQuestion(2, 1, false, visibleIf(1, "equals", "a"))
	t.Run("moving a source after its dependent is rejected", func(t *testing.T) {
		moved := first
		moved.Position = 5
		if err := savedQuestionVisibilityError([]domain.FormQuestion{first, second}, moved); err == nil {
			t.Fatal("expected an error")
		}
	})
	t.Run("rewriting a dependent into a cycle is rejected", func(t *testing.T) {
		looping := first
		looping.Settings = visibleIf(2, "any", nil)
		err := savedQuestionVisibilityError([]domain.FormQuestion{first, second}, looping)
		if err == nil {
			t.Fatal("expected a cycle error")
		}
	})
	t.Run("unrelated valid update passes", func(t *testing.T) {
		updated := second
		updated.Title = "new title"
		if err := savedQuestionVisibilityError([]domain.FormQuestion{first, second}, updated); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("referenced questions are detected", func(t *testing.T) {
		if !referencedByVisibility([]domain.FormQuestion{first, second}, 1) || referencedByVisibility([]domain.FormQuestion{first, second}, 2) {
			t.Fatal("referencedByVisibility is wrong")
		}
	})
	t.Run("publishing validates the whole form", func(t *testing.T) {
		if err := publishVisibilityError([]domain.FormQuestion{first, second}); err != nil {
			t.Fatal(err)
		}
		broken := []domain.FormQuestion{first, textQuestion(2, 1, false, visibleIf(77, "any", nil))}
		if err := publishVisibilityError(broken); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestEvaluateVisibility(t *testing.T) {
	rule := func(operator string, value any) visibilityRule {
		raw, _ := json.Marshal(value)
		if value == nil {
			raw = nil
		}
		return visibilityRule{QuestionID: 1, Operator: operator, Value: raw}
	}
	cases := []struct {
		name     string
		rule     visibilityRule
		answer   any
		answered bool
		want     bool
	}{
		{"equals string", rule("equals", "a"), "a", true, true},
		{"equals other string", rule("equals", "a"), "b", true, false},
		{"equals without an answer", rule("equals", "a"), nil, false, false},
		{"equals number", rule("equals", 3), json.Number("3"), true, true},
		{"equals number mismatch", rule("equals", 3), json.Number("4"), true, false},
		{"equals bool", rule("equals", true), true, true, true},
		{"equals type mismatch", rule("equals", "3"), json.Number("3"), true, false},
		{"equals single element list", rule("equals", "a"), []any{"a"}, true, true},
		{"equals multi element list", rule("equals", "a"), []any{"a", "b"}, true, false},
		{"not_equals other", rule("not_equals", "a"), "b", true, true},
		{"not_equals same", rule("not_equals", "a"), "a", true, false},
		{"not_equals without an answer", rule("not_equals", "a"), nil, false, true},
		{"contains in list", rule("contains", "b"), []any{"a", "b"}, true, true},
		{"contains missing in list", rule("contains", "c"), []any{"a", "b"}, true, false},
		{"contains substring ignoring case", rule("contains", "WORLD"), "Hello world", true, true},
		{"contains missing substring", rule("contains", "xyz"), "Hello world", true, false},
		{"contains on a number", rule("contains", "1"), json.Number("1"), true, false},
		{"contains without an answer", rule("contains", "a"), nil, false, false},
		{"any with text", rule("any", nil), "x", true, true},
		{"any with list", rule("any", nil), []any{"x"}, true, true},
		{"any with blank text", rule("any", nil), "  ", true, false},
		{"any with empty list", rule("any", nil), []any{}, true, false},
		{"any without an answer", rule("any", nil), nil, false, false},
		{"any with checked box", rule("any", nil), true, true, true},
		{"any with unchecked box", rule("any", nil), false, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evaluateVisibility(tc.rule, tc.answer, tc.answered); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestValidateSubmissionConditionalQuestions(t *testing.T) {
	gate := choiceQuestion(1, 0, "yes", "no")
	gate.Required = true
	detail := textQuestion(2, 1, true, visibleIf(1, "equals", "yes"))

	t.Run("a visible required question must be answered", func(t *testing.T) {
		_, err := validateSubmission([]domain.FormQuestion{gate, detail}, answersInput(map[uint]any{1: "yes"}))
		if !errors.Is(err, domain.ErrInvalidSubmission) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a hidden required question is not required", func(t *testing.T) {
		answers, err := validateSubmission([]domain.FormQuestion{gate, detail}, answersInput(map[uint]any{1: "no"}))
		if err != nil {
			t.Fatal(err)
		}
		if got := answeredIDs(answers); !got[1] || got[2] {
			t.Fatalf("answers = %v", got)
		}
	})
	t.Run("an answer to a hidden question is dropped", func(t *testing.T) {
		answers, err := validateSubmission([]domain.FormQuestion{gate, detail}, answersInput(map[uint]any{1: "no", 2: "stray"}))
		if err != nil {
			t.Fatal(err)
		}
		if got := answeredIDs(answers); got[2] {
			t.Fatalf("hidden answer was kept: %v", got)
		}
	})
	t.Run("an invalid answer to a hidden question is also dropped", func(t *testing.T) {
		answers, err := validateSubmission([]domain.FormQuestion{gate, detail}, answersInput(map[uint]any{1: "no", 2: 12345}))
		if err != nil || answeredIDs(answers)[2] {
			t.Fatalf("answers = %v, err = %v", answers, err)
		}
	})
	t.Run("a visible question is validated and stored", func(t *testing.T) {
		answers, err := validateSubmission([]domain.FormQuestion{gate, detail}, answersInput(map[uint]any{1: "yes", 2: "details"}))
		if err != nil || !answeredIDs(answers)[2] {
			t.Fatalf("answers = %v, err = %v", answers, err)
		}
	})
	t.Run("visibility chains through hidden questions", func(t *testing.T) {
		third := textQuestion(3, 2, true, visibleIf(2, "any", nil))
		questions := []domain.FormQuestion{gate, detail, third}
		answers, err := validateSubmission(questions, answersInput(map[uint]any{1: "no", 3: "stray"}))
		if err != nil {
			t.Fatal(err)
		}
		if got := answeredIDs(answers); got[2] || got[3] {
			t.Fatalf("answers = %v", got)
		}
		if _, err := validateSubmission(questions, answersInput(map[uint]any{1: "yes", 2: "d"})); !errors.Is(err, domain.ErrInvalidSubmission) {
			t.Fatalf("visible chain must require the third answer, err = %v", err)
		}
		answers, err = validateSubmission(questions, answersInput(map[uint]any{1: "yes", 2: "d", 3: "e"}))
		if err != nil || len(answers) != 3 {
			t.Fatalf("answers = %v, err = %v", answers, err)
		}
	})
	t.Run("a conditional section hides its dependants only", func(t *testing.T) {
		section := domain.FormQuestion{ID: 5, Type: domain.QuestionSection, Title: "s", Position: 1, Settings: visibleIf(1, "equals", "yes")}
		if _, err := validateSubmission([]domain.FormQuestion{gate, section}, answersInput(map[uint]any{1: "no"})); err != nil {
			t.Fatal(err)
		}
		if _, err := validateSubmission([]domain.FormQuestion{gate, section}, answersInput(map[uint]any{1: "no", 5: "x"})); !errors.Is(err, domain.ErrInvalidSubmission) {
			t.Fatalf("an answer to a static question is still rejected, err = %v", err)
		}
	})
	t.Run("a multiple choice condition uses contains", func(t *testing.T) {
		multi := domain.FormQuestion{
			ID: 1, Type: domain.QuestionMultipleChoice, Title: "m", Position: 0,
			Options: []domain.QuestionOption{{Value: "a", Label: "a"}, {Value: "b", Label: "b"}},
		}
		follow := textQuestion(2, 1, true, visibleIf(1, "contains", "b"))
		if _, err := validateSubmission([]domain.FormQuestion{multi, follow}, answersInput(map[uint]any{1: []string{"a"}})); err != nil {
			t.Fatal(err)
		}
		if _, err := validateSubmission([]domain.FormQuestion{multi, follow}, answersInput(map[uint]any{1: []string{"a", "b"}})); !errors.Is(err, domain.ErrInvalidSubmission) {
			t.Fatalf("err = %v", err)
		}
	})
}
