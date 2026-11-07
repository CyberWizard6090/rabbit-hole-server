package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"rabbit-hole-server/internal/domain"
)

const (
	visibilityInvalidCode  = "FORM_QUESTION_VISIBILITY_INVALID"
	visibilityCycleCode    = "FORM_QUESTION_VISIBILITY_CYCLE"
	questionReferencedCode = "FORM_QUESTION_REFERENCED"

	visibilityEquals    = "equals"
	visibilityNotEquals = "not_equals"
	visibilityContains  = "contains"
	visibilityAny       = "any"
)

type visibilityRule struct {
	QuestionID uint            `json:"question_id"`
	Operator   string          `json:"operator"`
	Value      json.RawMessage `json:"value,omitempty"`
}

func visibilityError(message string) error {
	return domain.NewFormError(visibilityInvalidCode, message)
}

func parseVisibility(settings json.RawMessage) (*visibilityRule, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(settings, &envelope); err != nil {
		return nil, nil
	}
	raw, ok := envelope["visible_if"]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nil
	}
	var rule visibilityRule
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&rule); err != nil {
		return nil, visibilityError("visible_if must be an object with question_id, operator and value")
	}
	switch rule.Operator {
	case visibilityEquals, visibilityNotEquals, visibilityContains:
		var value any
		if err := json.Unmarshal(rule.Value, &value); err != nil || !scalarVisibilityValue(value) {
			return nil, visibilityError("visible_if.value must be a string, number or boolean")
		}
	case visibilityAny:
		rule.Value = nil
	default:
		return nil, visibilityError("visible_if.operator must be one of equals, not_equals, contains, any")
	}
	if rule.QuestionID == 0 {
		return nil, visibilityError("visible_if.question_id is required")
	}
	return &rule, nil
}

func scalarVisibilityValue(value any) bool {
	switch value.(type) {
	case string, float64, bool:
		return true
	default:
		return false
	}
}

func orderedQuestions(questions []domain.FormQuestion) []domain.FormQuestion {
	ordered := append([]domain.FormQuestion(nil), questions...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Position != ordered[j].Position {
			return ordered[i].Position < ordered[j].Position
		}
		return questionOrderID(ordered[i]) < questionOrderID(ordered[j])
	})
	return ordered
}

func questionOrderID(question domain.FormQuestion) uint {
	if question.ID == 0 {
		return math.MaxUint32
	}
	return question.ID
}

func questionBefore(a, b domain.FormQuestion) bool {
	if a.Position != b.Position {
		return a.Position < b.Position
	}
	return questionOrderID(a) < questionOrderID(b)
}

func visibilityErrors(questions []domain.FormQuestion) map[uint]error {
	byID := make(map[uint]domain.FormQuestion, len(questions))
	rules := make(map[uint]*visibilityRule, len(questions))
	errs := make(map[uint]error)
	for _, question := range questions {
		byID[question.ID] = question
		rule, err := parseVisibility(question.Settings)
		if err != nil {
			errs[question.ID] = err
			continue
		}
		if rule != nil {
			rules[question.ID] = rule
		}
	}
	for id := range rules {
		if errs[id] != nil {
			continue
		}
		if err := checkVisibilityChain(id, rules); err != nil {
			errs[id] = err
		}
	}
	for id, rule := range rules {
		if errs[id] != nil {
			continue
		}
		question := byID[id]
		target, exists := byID[rule.QuestionID]
		switch {
		case !exists:
			errs[id] = visibilityError(fmt.Sprintf("visible_if refers to unknown question %d", rule.QuestionID))
		case isStaticQuestion(target.Type):
			errs[id] = visibilityError("visible_if cannot depend on a section, description or divider")
		case !questionBefore(target, question):
			errs[id] = visibilityError("visible_if must refer to a question placed earlier in the form")
		default:
			if err := checkVisibilityValue(*rule, target); err != nil {
				errs[id] = err
			}
		}
	}
	return errs
}

func checkVisibilityChain(start uint, rules map[uint]*visibilityRule) error {
	seen := map[uint]struct{}{start: {}}
	current := start
	for {
		rule, ok := rules[current]
		if !ok {
			return nil
		}
		next := rule.QuestionID
		if _, loop := seen[next]; loop {
			if next == start && current == start {
				return domain.NewFormError(visibilityCycleCode, "a question cannot depend on itself")
			}
			return domain.NewFormError(visibilityCycleCode, "visible_if rules form a cycle")
		}
		seen[next] = struct{}{}
		current = next
	}
}

func checkVisibilityValue(rule visibilityRule, target domain.FormQuestion) error {
	if rule.Operator == visibilityAny {
		return nil
	}
	var value any
	_ = json.Unmarshal(rule.Value, &value)
	text, isText := value.(string)
	switch target.Type {
	case domain.QuestionSingleChoice, domain.QuestionDropdown, domain.QuestionMultipleChoice:
		if !isText || !questionHasOption(target.Options, text) {
			return visibilityError("visible_if.value must be one of the options of the referenced question")
		}
		if rule.Operator == visibilityContains && target.Type != domain.QuestionMultipleChoice {
			return visibilityError("contains only applies to multiple choice and text questions")
		}
	case domain.QuestionCheckbox:
		if _, isBool := value.(bool); !isBool || rule.Operator == visibilityContains {
			return visibilityError("a checkbox condition needs a boolean value and equals or not_equals")
		}
	case domain.QuestionNumber, domain.QuestionRating, domain.QuestionScale:
		if _, isNumber := value.(float64); !isNumber || rule.Operator == visibilityContains {
			return visibilityError("a numeric condition needs a number value and equals or not_equals")
		}
	case domain.QuestionFile:
		return visibilityError("a file question can only be used with the any operator")
	default:
		if !isText {
			return visibilityError("visible_if.value must be a string for this question")
		}
	}
	return nil
}

func savedQuestionVisibilityError(existing []domain.FormQuestion, saved domain.FormQuestion) error {
	candidate := make([]domain.FormQuestion, 0, len(existing)+1)
	for _, question := range existing {
		if saved.ID == 0 || question.ID != saved.ID {
			candidate = append(candidate, question)
		}
	}
	candidate = append(candidate, saved)
	errs := visibilityErrors(candidate)
	if err := errs[saved.ID]; err != nil {
		return err
	}
	if saved.ID == 0 {
		return nil
	}
	for _, question := range candidate {
		if question.ID == saved.ID {
			continue
		}
		rule, _ := parseVisibility(question.Settings)
		if rule != nil && rule.QuestionID == saved.ID && errs[question.ID] != nil {
			return errs[question.ID]
		}
	}
	return nil
}

func publishVisibilityError(questions []domain.FormQuestion) error {
	errs := visibilityErrors(questions)
	for _, question := range orderedQuestions(questions) {
		if err := errs[question.ID]; err != nil {
			return err
		}
	}
	return nil
}

func referencedByVisibility(questions []domain.FormQuestion, questionID uint) bool {
	for _, question := range questions {
		if question.ID == questionID {
			continue
		}
		if rule, _ := parseVisibility(question.Settings); rule != nil && rule.QuestionID == questionID {
			return true
		}
	}
	return false
}

func questionVisible(question domain.FormQuestion, answers map[uint]any) bool {
	rule, err := parseVisibility(question.Settings)
	if err != nil || rule == nil {
		return true
	}
	answer, answered := answers[rule.QuestionID]
	return evaluateVisibility(*rule, answer, answered)
}

func evaluateVisibility(rule visibilityRule, answer any, answered bool) bool {
	var expected any
	if len(rule.Value) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(rule.Value))
		decoder.UseNumber()
		_ = decoder.Decode(&expected)
	}
	switch rule.Operator {
	case visibilityAny:
		if !answered || isEmptySubmissionValue(answer) {
			return false
		}
		if flag, isBool := answer.(bool); isBool {
			return flag
		}
		return true
	case visibilityEquals:
		return answered && visibilityEqual(answer, expected)
	case visibilityNotEquals:
		return !answered || !visibilityEqual(answer, expected)
	case visibilityContains:
		if !answered {
			return false
		}
		switch typed := answer.(type) {
		case []any:
			for _, item := range typed {
				if visibilityEqual(item, expected) {
					return true
				}
			}
		case string:
			needle, isText := expected.(string)
			return isText && strings.Contains(strings.ToLower(typed), strings.ToLower(needle))
		}
		return false
	default:
		return true
	}
}

func visibilityEqual(answer, expected any) bool {
	if list, isList := answer.([]any); isList {
		return len(list) == 1 && visibilityEqual(list[0], expected)
	}
	switch typed := answer.(type) {
	case string:
		text, ok := expected.(string)
		return ok && typed == text
	case bool:
		flag, ok := expected.(bool)
		return ok && typed == flag
	case json.Number:
		left, ok := questionNumber(typed)
		if !ok {
			return false
		}
		right, ok := questionNumber(expected)
		return ok && left == right
	default:
		return false
	}
}
