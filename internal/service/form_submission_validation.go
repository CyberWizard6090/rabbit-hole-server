package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/mail"
	"net/url"
	"sort"
	"time"
	"unicode/utf8"

	"rabbit-hole-server/internal/domain"
)

const (
	submissionFieldPrefix = "answers."
	maxAnswerBytes        = 1 << 18
)

func answerField(questionID uint) string {
	return fmt.Sprintf("%s%d", submissionFieldPrefix, questionID)
}

type answerIssue struct {
	reason string
	params map[string]any
}

func validateSubmission(questions []domain.FormQuestion, input map[uint]json.RawMessage) ([]domain.SubmissionAnswer, error) {
	ordered := orderedQuestions(questions)
	known := make(map[uint]struct{}, len(ordered))
	for _, question := range ordered {
		known[question.ID] = struct{}{}
	}
	var problems []domain.FieldProblem
	unknown := make([]uint, 0)
	for id := range input {
		if _, ok := known[id]; !ok {
			unknown = append(unknown, id)
		}
	}
	sort.Slice(unknown, func(i, j int) bool { return unknown[i] < unknown[j] })
	for _, id := range unknown {
		problems = append(problems, domain.FieldProblem{Field: answerField(id), Reason: "UNKNOWN_QUESTION"})
	}
	answers := make([]domain.SubmissionAnswer, 0, len(input))
	effective := make(map[uint]any, len(input))
	invalid := make(map[uint]struct{})
	for _, question := range ordered {
		raw, exists := input[question.ID]
		if isStaticQuestion(question.Type) {
			if exists {
				problems = append(problems, domain.FieldProblem{Field: answerField(question.ID), Reason: "NOT_ALLOWED"})
			}
			continue
		}
		if rule, _ := parseVisibility(question.Settings); rule != nil {
			if _, broken := invalid[rule.QuestionID]; broken {
				invalid[question.ID] = struct{}{}
				continue
			}
		}
		if !questionVisible(question, effective) {
			continue
		}
		if !exists {
			if question.Required {
				problems = append(problems, domain.FieldProblem{Field: answerField(question.ID), Reason: "REQUIRED"})
				invalid[question.ID] = struct{}{}
			}
			continue
		}
		value, normalized, issue := decodeAnswer(question, raw)
		if issue != nil {
			problems = append(problems, domain.FieldProblem{Field: answerField(question.ID), Reason: issue.reason, Params: issue.params})
			invalid[question.ID] = struct{}{}
			continue
		}
		effective[question.ID] = value
		answers = append(answers, domain.SubmissionAnswer{QuestionID: question.ID, Value: normalized})
	}
	if len(problems) > 0 {
		return nil, domain.InvalidFields(domain.ErrInvalidSubmission, problems)
	}
	return answers, nil
}

func decodeAnswer(question domain.FormQuestion, raw json.RawMessage) (any, json.RawMessage, *answerIssue) {
	if len(raw) > maxAnswerBytes {
		return nil, nil, &answerIssue{reason: "TOO_LARGE", params: map[string]any{"max_bytes": maxAnswerBytes}}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if len(raw) == 0 || decoder.Decode(&value) != nil {
		return nil, nil, &answerIssue{reason: "INVALID_VALUE"}
	}
	if isEmptySubmissionValue(value) {
		if question.Required {
			return nil, nil, &answerIssue{reason: "REQUIRED"}
		}
		return nil, nil, &answerIssue{reason: "EMPTY"}
	}
	if issue := checkAnswer(question, value); issue != nil {
		return nil, nil, issue
	}
	normalized, err := json.Marshal(value)
	if err != nil {
		return nil, nil, &answerIssue{reason: "INVALID_VALUE"}
	}
	return value, normalized, nil
}

func wrongType(expected string) *answerIssue {
	return &answerIssue{reason: "INVALID_TYPE", params: map[string]any{"expected": expected}}
}

func wrongFormat(format string) *answerIssue {
	return &answerIssue{reason: "INVALID_FORMAT", params: map[string]any{"format": format}}
}

func checkAnswer(question domain.FormQuestion, value any) *answerIssue {
	switch question.Type {
	case domain.QuestionShortText, domain.QuestionLongText:
		text, ok := value.(string)
		if !ok {
			return wrongType("string")
		}
		return textLengthIssue(question, text)
	case domain.QuestionPhone:
		if _, ok := value.(string); !ok {
			return wrongType("string")
		}
		return nil
	case domain.QuestionEmail:
		text, ok := value.(string)
		if !ok {
			return wrongType("string")
		}
		if address, err := mail.ParseAddress(text); err != nil || address.Address != text {
			return wrongFormat("email")
		}
		return nil
	case domain.QuestionURL:
		text, ok := value.(string)
		if !ok {
			return wrongType("string")
		}
		if parsed, err := url.ParseRequestURI(text); err != nil ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			return wrongFormat("url")
		}
		return nil
	case domain.QuestionDate:
		text, ok := value.(string)
		if !ok {
			return wrongType("string")
		}
		if _, err := time.Parse("2006-01-02", text); err != nil {
			return wrongFormat("YYYY-MM-DD")
		}
		return nil
	case domain.QuestionDateTime:
		text, ok := value.(string)
		if !ok {
			return wrongType("string")
		}
		if _, err := time.Parse(time.RFC3339, text); err != nil {
			return wrongFormat("RFC3339")
		}
		return nil
	case domain.QuestionNumber, domain.QuestionRating, domain.QuestionScale:
		return numericIssue(question, value)
	case domain.QuestionCheckbox:
		if _, ok := value.(bool); !ok {
			return wrongType("boolean")
		}
		return nil
	case domain.QuestionSingleChoice, domain.QuestionDropdown:
		text, ok := value.(string)
		if !ok {
			return wrongType("string")
		}
		if !questionHasOption(question.Options, text) {
			return &answerIssue{reason: "INVALID_CHOICE", params: map[string]any{"allowed": optionValues(question.Options)}}
		}
		return nil
	case domain.QuestionMultipleChoice:
		return multipleChoiceIssue(question, value)
	case domain.QuestionFile:
		return fileAnswerIssue(question, value)
	default:
		return &answerIssue{reason: "INVALID_VALUE"}
	}
}

func textLengthIssue(question domain.FormQuestion, text string) *answerIssue {
	settings := questionSettingsMap(question.Settings)
	length := float64(utf8.RuneCountInString(text))
	if low, ok := settingNumberValue(settings, "min_length"); ok && length < low {
		return &answerIssue{reason: "TOO_SHORT", params: map[string]any{"min": int(low)}}
	}
	if high, ok := settingNumberValue(settings, "max_length"); ok && length > high {
		return &answerIssue{reason: "TOO_LONG", params: map[string]any{"max": int(high)}}
	}
	return nil
}

func numericIssue(question domain.FormQuestion, value any) *answerIssue {
	number, ok := questionNumber(value)
	if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
		return wrongType("number")
	}
	settings := questionSettingsMap(question.Settings)
	low, high := math.Inf(-1), math.Inf(1)
	switch question.Type {
	case domain.QuestionRating, domain.QuestionScale:
		if number != math.Trunc(number) {
			return wrongType("integer")
		}
		low, high = integerBounds(question.Type, map[string]float64{})
	}
	if bound, ok := settingNumberValue(settings, "min"); ok {
		low = bound
	}
	if bound, ok := settingNumberValue(settings, "max"); ok {
		high = bound
	}
	if number >= low && number <= high {
		return nil
	}
	params := map[string]any{}
	if !math.IsInf(low, -1) {
		params["min"] = low
	}
	if !math.IsInf(high, 1) {
		params["max"] = high
	}
	return &answerIssue{reason: "OUT_OF_RANGE", params: params}
}

func multipleChoiceIssue(question domain.FormQuestion, value any) *answerIssue {
	values, ok := value.([]any)
	if !ok {
		return wrongType("array")
	}
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		text, ok := raw.(string)
		if !ok {
			return wrongType("string")
		}
		if !questionHasOption(question.Options, text) {
			return &answerIssue{reason: "INVALID_CHOICE", params: map[string]any{
				"allowed": optionValues(question.Options), "value": text,
			}}
		}
		if _, duplicate := seen[text]; duplicate {
			return &answerIssue{reason: "DUPLICATE", params: map[string]any{"value": text}}
		}
		seen[text] = struct{}{}
	}
	settings := questionSettingsMap(question.Settings)
	if low, ok := settingNumberValue(settings, "min_selected"); ok && float64(len(values)) < low {
		return &answerIssue{reason: "TOO_FEW", params: map[string]any{"min": int(low)}}
	}
	if high, ok := settingNumberValue(settings, "max_selected"); ok && float64(len(values)) > high {
		return &answerIssue{reason: "TOO_MANY", params: map[string]any{"max": int(high)}}
	}
	return nil
}

func fileAnswerIssue(question domain.FormQuestion, value any) *answerIssue {
	values, ok := value.([]any)
	if !ok {
		return wrongType("array")
	}
	if limit := questionMaxFiles(question.Settings); len(values) > limit {
		return &answerIssue{reason: "TOO_MANY", params: map[string]any{"max": limit}}
	}
	seen := make(map[string]struct{}, len(values))
	for _, raw := range values {
		text, ok := raw.(string)
		if !ok || !publicIDPattern.MatchString(text) {
			return &answerIssue{reason: "INVALID_FILE"}
		}
		if _, duplicate := seen[text]; duplicate {
			return &answerIssue{reason: "DUPLICATE"}
		}
		seen[text] = struct{}{}
	}
	return nil
}
