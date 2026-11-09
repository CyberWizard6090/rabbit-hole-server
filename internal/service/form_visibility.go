package service

import (
	"bytes"
	"encoding/json"
	"errors"
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

const (
	visibilityRuleField     = "settings.visible_if"
	visibilityQuestionField = visibilityRuleField + ".question_id"
	visibilityOperatorField = visibilityRuleField + ".operator"
	visibilityValueField    = visibilityRuleField + ".value"
)

func visibilityError(field, reason, message string, params map[string]any) error {
	return domain.NewFormFieldError(visibilityInvalidCode, message, field, reason, params)
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
		return nil, visibilityError(visibilityRuleField, "INVALID_TYPE", "visible_if must be an object with question_id, operator and value",
			map[string]any{"expected": "object"})
	}
	switch rule.Operator {
	case visibilityEquals, visibilityNotEquals, visibilityContains:
		var value any
		if err := json.Unmarshal(rule.Value, &value); err != nil || !scalarVisibilityValue(value) {
			return nil, visibilityError(visibilityValueField, "INVALID_TYPE", "visible_if.value must be a string, number or boolean",
				map[string]any{"expected": "string, number or boolean"})
		}
	case visibilityAny:
		rule.Value = nil
	default:
		return nil, visibilityError(visibilityOperatorField, "INVALID_CHOICE", "visible_if.operator must be one of equals, not_equals, contains, any",
			map[string]any{"allowed": []string{visibilityEquals, visibilityNotEquals, visibilityContains, visibilityAny}})
	}
	if rule.QuestionID == 0 {
		return nil, visibilityError(visibilityQuestionField, "REQUIRED", "visible_if.question_id is required", nil)
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
			errs[id] = visibilityError(visibilityQuestionField, "NOT_FOUND", fmt.Sprintf("visible_if refers to unknown question %d", rule.QuestionID),
				map[string]any{"depends_on": rule.QuestionID})
		case isStaticQuestion(target.Type):
			errs[id] = visibilityError(visibilityQuestionField, "NOT_ALLOWED", "visible_if cannot depend on a section, description or divider",
				map[string]any{"depends_on": rule.QuestionID, "question_type": string(target.Type)})
		case !questionBefore(target, question):
			errs[id] = visibilityError(visibilityQuestionField, "NOT_EARLIER", "visible_if must refer to a question placed earlier in the form",
				map[string]any{"depends_on": rule.QuestionID})
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
				return domain.NewFormFieldError(visibilityCycleCode, "a question cannot depend on itself",
					visibilityQuestionField, "SELF_REFERENCE", nil)
			}
			return domain.NewFormFieldError(visibilityCycleCode, "visible_if rules form a cycle",
				visibilityQuestionField, "CYCLE", map[string]any{"depends_on": rule.QuestionID})
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
			return visibilityError(visibilityValueField, "INVALID_CHOICE", "visible_if.value must be one of the options of the referenced question",
				map[string]any{"allowed": optionValues(target.Options)})
		}
		if rule.Operator == visibilityContains && target.Type != domain.QuestionMultipleChoice {
			return visibilityError(visibilityOperatorField, "NOT_SUPPORTED", "contains only applies to multiple choice and text questions",
				map[string]any{"question_type": string(target.Type)})
		}
	case domain.QuestionCheckbox:
		if _, isBool := value.(bool); !isBool {
			return visibilityError(visibilityValueField, "INVALID_TYPE", "a checkbox condition needs a boolean value",
				map[string]any{"expected": "boolean"})
		}
		if rule.Operator == visibilityContains {
			return visibilityError(visibilityOperatorField, "NOT_SUPPORTED", "a checkbox condition needs equals or not_equals",
				map[string]any{"question_type": string(target.Type)})
		}
	case domain.QuestionNumber, domain.QuestionRating, domain.QuestionScale:
		if _, isNumber := value.(float64); !isNumber {
			return visibilityError(visibilityValueField, "INVALID_TYPE", "a numeric condition needs a number value",
				map[string]any{"expected": "number"})
		}
		if rule.Operator == visibilityContains {
			return visibilityError(visibilityOperatorField, "NOT_SUPPORTED", "a numeric condition needs equals or not_equals",
				map[string]any{"question_type": string(target.Type)})
		}
	case domain.QuestionFile:
		return visibilityError(visibilityOperatorField, "NOT_SUPPORTED", "a file question can only be used with the any operator",
			map[string]any{"question_type": string(target.Type)})
	default:
		if !isText {
			return visibilityError(visibilityValueField, "INVALID_TYPE", "visible_if.value must be a string for this question",
				map[string]any{"expected": "string"})
		}
	}
	return nil
}

func optionValues(options []domain.QuestionOption) []string {
	values := make([]string, len(options))
	for i, option := range options {
		values[i] = option.Value
	}
	return values
}

func placeNewQuestion(existing []domain.FormQuestion, saved domain.FormQuestion) ([]domain.FormQuestion, domain.FormQuestion) {
	placed := append([]domain.FormQuestion(nil), existing...)
	if saved.Position == domain.AppendQuestionPosition {
		next := 0
		for _, question := range placed {
			if question.Position >= next {
				next = question.Position + 1
			}
		}
		saved.Position = next
		return placed, saved
	}
	taken := false
	for _, question := range placed {
		if question.Position == saved.Position {
			taken = true
		}
	}
	if taken {
		for i := range placed {
			if placed[i].Position >= saved.Position {
				placed[i].Position++
			}
		}
	}
	return placed, saved
}

func savedQuestionVisibilityError(existing []domain.FormQuestion, saved domain.FormQuestion) error {
	candidate := make([]domain.FormQuestion, 0, len(existing)+1)
	for _, question := range existing {
		if saved.ID == 0 || question.ID != saved.ID {
			candidate = append(candidate, question)
		} else if saved.Position == domain.AppendQuestionPosition {
			saved.Position = question.Position
		}
	}
	if saved.ID == 0 {
		candidate, saved = placeNewQuestion(candidate, saved)
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
	for index, question := range orderedQuestions(questions) {
		err := errs[question.ID]
		if err == nil {
			continue
		}
		var formErr *domain.FormError
		if errors.As(err, &formErr) {
			prefixed := formErr.WithPrefix(fmt.Sprintf("questions[%d].", index))
			for i := range prefixed.Problems {
				params := map[string]any{"question_id": question.ID}
				for key, value := range prefixed.Problems[i].Params {
					params[key] = value
				}
				prefixed.Problems[i].Params = params
			}
			return prefixed
		}
		return err
	}
	return nil
}

func reorderVisibilityError(questions []domain.FormQuestion, ids []uint) error {
	byID := make(map[uint]domain.FormQuestion, len(questions))
	for _, question := range questions {
		byID[question.ID] = question
	}
	candidate := make([]domain.FormQuestion, 0, len(ids))
	for position, id := range ids {
		question := byID[id]
		question.Position = position
		candidate = append(candidate, question)
	}
	errs := visibilityErrors(candidate)
	for _, question := range candidate {
		if errs[question.ID] == nil {
			continue
		}
		rule, _ := parseVisibility(question.Settings)
		params := map[string]any{"question_id": question.ID}
		if rule != nil {
			params["depends_on"] = rule.QuestionID
		}
		return domain.NewFormFieldError(visibilityInvalidCode,
			"the new order would put a question before the question its visible_if rule depends on",
			"ids", "RULE_NOT_EARLIER", params)
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
